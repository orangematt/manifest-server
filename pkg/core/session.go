// (c) Copyright 2017-2023 Matt Messier

package core

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/jumptown-skydiving/manifest-server/pkg/db"
	"github.com/orangematt/siwa"
)

func (c *Controller) BeginDatabaseTransaction() (*sql.Tx, error) {
	return c.db.Begin()
}

func (c *Controller) CommitDatabaseTransaction(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}

func (c *Controller) AbortDatabaseTransaction(tx *sql.Tx) error {
	return tx.Rollback()
}

func (c *Controller) NewSession(
	tx *sql.Tx,
	user *db.User,
	accessToken string,
	refreshToken string,
	identityToken string,
	nonce string,
	provider string,
) (*db.Session, error) {
	now := time.Now()
	refreshTime := now.Add(24 * time.Hour)
	expireTime := now.Add(6 * 30 * 24 * time.Hour)

	return c.db.CreateSession(tx, user, refreshTime, expireTime,
		refreshToken, accessToken, identityToken, nonce, provider)
}

func (c *Controller) LookupSession(
	ctx context.Context,
	tx *sql.Tx,
	sessionid string,
) (*db.Session, error) {
	session, err := c.db.LookupSession(tx, sessionid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "LookupSession(%q) -> %v\n", sessionid, err)
		return nil, err
	}

	now := time.Now()
	if session.ExpireTime.Before(now) {
		// session has expired; delete it
		fmt.Fprintf(os.Stderr, "LookupSession(%q) -> session has expired\n", sessionid)
		return nil, c.db.DeleteSession(tx, sessionid)
	}
	if session.RefreshTime.Before(now) {
		// refresh token has expired; refresh it
		switch session.Provider {
		case "siwa":
			if c.siwa == nil {
				fmt.Fprintf(os.Stderr, "Session token refresh: no Sign In With Apple instance\n")
				return nil, c.db.DeleteSession(tx, sessionid)
			}
			r, err := c.siwa.ValidateRefreshToken(ctx,
				session.Nonce, session.RefreshToken)
			if err != nil {
				if _, ok := err.(siwa.ErrorResponse); ok {
					fmt.Fprintf(os.Stderr, "Session token refresh SIWA error: %v\n", err)
					return nil, c.db.DeleteSession(tx, sessionid)
				}
				fmt.Fprintf(os.Stderr, "Session token refresh: %v\n", err)
				return nil, err
			}
			// ignore r.ExpiresIn - not sure what we'll get back for
			// this; it's not well documented by Apple. But Apple
			// does say do not refresh more than once every 24 hours
			// so that's what we'll use here. Looks like 3600 is
			// what Apple returns here, which is weird.
			//
			// Note also that we use session.RefreshToken here
			// instead of r.RefreshToken. This is because Apple's
			// servers do not return the refresh token when validing
			// an existing refresh token, indicating that we should
			// just keep using the same token forever.
			expiresIn := 24 * time.Hour
			err = c.db.UpdateSessionTokens(tx, session,
				r.AccessToken, session.RefreshToken, r.IdentityToken,
				expiresIn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Session token refresh update tokens error: %v\n", err)
				return nil, err
			}
		default:
			return nil, c.db.DeleteSession(tx, sessionid)
		}
	}

	return session, nil
}

func (c *Controller) DeleteSession(
	ctx context.Context,
	tx *sql.Tx,
	sessionid string,
) error {
	session, err := c.db.LookupSession(tx, sessionid)
	if err != nil {
		return err
	}

	switch session.Provider {
	case "siwa":
		if c.siwa != nil {
			_ = c.siwa.RevokeToken(ctx, session.RefreshToken, "refresh_token")
			_ = c.siwa.RevokeToken(ctx, session.AccessToken, "access_token")
		}
	}

	return c.db.DeleteSession(tx, sessionid)
}

func (c *Controller) CreateUser(
	tx *sql.Tx,
	userid string,
	givenName string,
	familyName string,
	email string,
	isPrivateEmail bool,
	isEmailVerified bool,
) (*db.User, error) {
	return c.db.CreateUser(tx, userid, givenName, familyName, email, isPrivateEmail, isEmailVerified)
}

func (c *Controller) LookupUser(tx *sql.Tx, userid string) (*db.User, error) {
	return c.db.LookupUser(tx, userid)
}

func (c *Controller) QueryRoles(tx *sql.Tx, user *db.User) ([]string, error) {
	return c.db.QueryRoles(tx, user)
}
