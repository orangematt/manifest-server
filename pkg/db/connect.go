// (c) Copyright 2017-2022 Matt Messier

package db

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jumptown-skydiving/manifest-server/pkg/settings"
)

type User struct {
	ID              string
	GivenName       string
	FamilyName      string
	Email           string
	IsPrivateEmail  bool
	IsEmailVerified bool
	CreateTime      time.Time

	db interface{}
	_  struct{}
}

type Session struct {
	ID            string
	UserID        string
	Nonce         string
	RefreshToken  string
	AccessToken   string
	IdentityToken string
	Provider      string
	CreateTime    time.Time
	RefreshTime   time.Time
	ExpireTime    time.Time

	db interface{}
	_  struct{}
}

var (
	ErrInvalidUserID = errors.New("invalid user ID")
)

type Connection interface {
	Close()
	Begin() (*sql.Tx, error)

	CreateUser(
		tx *sql.Tx,
		userid, givenName, familyName, email string,
		isPrivateEmail, isEmailVerified bool,
	) (*User, error)
	DeleteUser(tx *sql.Tx, userid string) error
	LookupUser(tx *sql.Tx, userid string) (*User, error)

	CreateSession(
		tx *sql.Tx,
		user *User,
		refreshTime, expireTime time.Time,
		refreshToken, accessToken, identityToken string,
		nonce string,
		provider string,
	) (*Session, error)
	DeleteSession(tx *sql.Tx, sessionid string) error
	LookupSession(tx *sql.Tx, sessionid string) (*Session, error)
	UpdateSessionTokens(
		tx *sql.Tx,
		session *Session,
		accessToken, refreshToken, identityToken string,
		expiresIn time.Duration,
	) error

	AddRole(tx *sql.Tx, user *User, role string) error
	RemoveRole(tx *sql.Tx, user *User, role string) error
	QueryRoles(tx *sql.Tx, user *User) ([]string, error)
}

func Connect(settings *settings.Settings) (Connection, error) {
	var (
		c   Connection
		err error
	)

	switch settings.DatabaseDriver() {
	case "sqlite3":
		c, err = connectViaSQLite3(settings)
	default:
		err = fmt.Errorf("unrecognized database driver %q",
			settings.DatabaseDriver())
	}
	if err != nil {
		return nil, err
	}

	return c, err
}

func NewSessionID(userid string) string {
	var b bytes.Buffer
	b.WriteString(userid)

	nano := time.Now().UnixNano()
	b.WriteString(strconv.FormatInt(nano, 10))

	r := make([]byte, 64)
	rand.Read(r)
	b.Write(r)

	h := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(h[:])
}
