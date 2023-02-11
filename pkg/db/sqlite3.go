// (c) Copyright 2017-2022 Matt Messier

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jumptown-skydiving/manifest-server/pkg/settings"

	_ "github.com/mattn/go-sqlite3"
)

type SQLite3 struct {
	c        *sql.DB
	settings *settings.Settings
}

const createUsersTableSQLite3 = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER NOT NULL PRIMARY KEY ASC AUTOINCREMENT,
	userid TEXT NOT NULL UNIQUE,
	given_name TEXT,
	family_name TEXT,
	email TEXT,
	is_private_email INTEGER NOT NULL DEFAULT 0,
	is_email_verified INTEGER NOT NULL DEFAULT 0,
	create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE UNIQUE INDEX IF NOT EXISTS users_userid ON users (userid);
`

const createSessionsTableSQLite3 = `
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER NOT NULL PRIMARY KEY ASC AUTOINCREMENT,
	sessionid TEXT NOT NULL UNIQUE,
	userid INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	refresh_time TIMESTAMP NOT NULL,
	expire_time TIMESTAMP NOT NULL,
	refresh_token TEXT NOT NULL,
	access_token TEXT NOT NULL,
	identity_token TEXT NOT NULL,
	nonce TEXT NOT NULL,
	provider TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS sessions_sessionid ON sessions (sessionid);
`

const createUsersRolesTableSQLite3 = `
CREATE TABLE IF NOT EXISTS roles (
	id INTEGER NOT NULL PRIMARY KEY ASC AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE);
INSERT OR IGNORE INTO roles (name) VALUES ("admin"), ("pilot");
CREATE TABLE IF NOT EXISTS users_roles (
	userid INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	roleid INTEGER NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
	PRIMARY KEY (userid, roleid) ON CONFLICT IGNORE);
CREATE INDEX IF NOT EXISTS users_roles_userid ON users_roles (userid);
`

type userSQLite3 struct {
	rowid int64
}

type sessionSQLite3 struct {
	rowid  int64
	userid int64
}

func connectViaSQLite3(settings *settings.Settings) (*SQLite3, error) {
	dsn := fmt.Sprintf("file:%s?mode=rwc", settings.DatabaseFilename())

	c, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	_, err = c.Exec(createUsersTableSQLite3)
	if err != nil {
		c.Close()
		return nil, err
	}

	_, err = c.Exec(createSessionsTableSQLite3)
	if err != nil {
		c.Close()
		return nil, err
	}

	_, err = c.Exec(createUsersRolesTableSQLite3)
	if err != nil {
		c.Close()
		return nil, err
	}

	db := SQLite3{
		c:        c,
		settings: settings,
	}
	return &db, nil
}

func (db *SQLite3) Close() {
	db.c.Close()
}

func (db *SQLite3) Begin() (*sql.Tx, error) {
	return db.c.Begin()
}

func (db *SQLite3) userFromRow(r *sql.Row) (*User, error) {
	if err := r.Err(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	var (
		u                            User
		ui                           userSQLite3
		givenName, familyName, email sql.NullString
	)
	err := r.Scan(&ui.rowid, &u.ID, &givenName, &familyName, &email,
		&u.IsPrivateEmail, &u.IsEmailVerified, &u.CreateTime)
	if err != nil {
		return nil, err
	}
	if givenName.Valid {
		u.GivenName = givenName.String
	}
	if familyName.Valid {
		u.FamilyName = familyName.String
	}
	if email.Valid {
		u.Email = email.String
	}
	u.db = ui
	return &u, nil
}

func (db *SQLite3) CreateUser(
	tx *sql.Tx,
	userid, givenName, familyName, email string,
	isPrivateEmail, isEmailVerified bool,
) (*User, error) {
	sqlGivenName := sql.NullString{
		String: givenName,
		Valid:  givenName != "",
	}
	sqlFamilyName := sql.NullString{
		String: familyName,
		Valid:  familyName != "",
	}
	sqlEmail := sql.NullString{
		String: email,
		Valid:  email != "",
	}
	var changes []string
	if sqlGivenName.Valid {
		changes = append(changes, "given_name = $2")
	}
	if sqlFamilyName.Valid {
		changes = append(changes, "family_name = $3")
	}
	if sqlEmail.Valid {
		changes = append(changes, "email = $4")
	}
	changes = append(changes, "is_private_email = $5")
	changes = append(changes, "is_email_verified = $6")

	stmt := "INSERT INTO users (userid, given_name, family_name, email, is_private_email, is_email_verified) " +
		"VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT(userid) DO "
	if len(changes) > 0 {
		stmt += "UPDATE SET " + strings.Join(changes, ", ")
	} else {
		stmt += "NOTHING"
	}
	stmt = stmt + " RETURNING *;"

	r := tx.QueryRow(stmt, userid, sqlGivenName, sqlFamilyName, sqlEmail, isPrivateEmail, isEmailVerified)
	return db.userFromRow(r)
}

func (db *SQLite3) DeleteUser(tx *sql.Tx, userid string) error {
	_, err := tx.Exec("DELETE FROM users WHERE userid = $1;", userid)
	return err
}

func (db *SQLite3) LookupUser(tx *sql.Tx, userid string) (*User, error) {
	r := tx.QueryRow("SELECT * FROM users WHERE userid = $1;", userid)
	return db.userFromRow(r)
}

func (db *SQLite3) sessionFromRow(r *sql.Row) (*Session, error) {
	if err := r.Err(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	var (
		s  Session
		si sessionSQLite3
	)
	err := r.Scan(&si.rowid, &s.ID, &si.userid, &s.CreateTime,
		&s.RefreshTime, &s.ExpireTime, &s.RefreshToken, &s.AccessToken,
		&s.IdentityToken, &s.Nonce, &s.Provider)
	if err != nil {
		return nil, err
	}
	s.db = si
	return &s, nil
}

func (db *SQLite3) CreateSession(
	tx *sql.Tx,
	user *User,
	refreshTime, expireTime time.Time,
	refreshToken, accessToken, identityToken string,
	nonce string,
	provider string,
) (*Session, error) {
	sessionid := NewSessionID(user.ID)

	ui, ok := user.db.(userSQLite3)
	if !ok || ui.rowid == 0 {
		return nil, ErrInvalidUserID
	}

	stmt := "INSERT INTO sessions (sessionid, userid, refresh_time, expire_time, refresh_token, access_token, identity_token, nonce, provider) " +
		"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) " +
		"RETURNING *;"

	r := tx.QueryRow(stmt, sessionid, ui.rowid, refreshTime, expireTime,
		refreshToken, accessToken, identityToken, nonce, provider)
	session, err := db.sessionFromRow(r)
	session.UserID = user.ID
	return session, err
}

func (db *SQLite3) DeleteSession(tx *sql.Tx, sessionid string) error {
	_, err := tx.Exec("DELETE FROM sessions where sessionid = $1;", sessionid)
	return err
}

func (db *SQLite3) LookupSession(tx *sql.Tx, sessionid string) (*Session, error) {
	// The proper thing to do here would be to use an INNER JOIN, but given
	// the way that the Go SQL API works, that would end up meaning we'd
	// have to duplicate db.userFromRow as part of db.sessionFromRow, which
	// really isn't desirable.
	//
	// So maybe two queries is a bit more expensive, but we're not talking
	// enterprise level stuff here. The expected amount of traffic for
	// looking up sessions and corresponding users ought to be extremely
	// low, so make two queries to keep the Go code cleaner.

	r := tx.QueryRow("SELECT * FROM sessions WHERE sessionid = $1;", sessionid)
	session, err := db.sessionFromRow(r)
	if err != nil {
		return nil, err
	}

	si, ok := session.db.(sessionSQLite3)
	if !ok || si.userid == 0 {
		return nil, ErrInvalidUserID
	}

	r = tx.QueryRow("SELECT userid FROM users WHERE id = $1;", si.userid)
	if err = r.Scan(&session.UserID); err != nil {
		return nil, err
	}

	return session, err
}

func (db *SQLite3) UpdateSessionTokens(
	tx *sql.Tx,
	session *Session,
	accessToken, refreshToken, identityToken string,
	expiresIn time.Duration,
) error {
	refreshTime := time.Now().Add(expiresIn)
	_, err := tx.Exec("UPDATE sessions SET access_token = $1, refresh_token = $2, identity_token = $3, refresh_time = $4;",
		accessToken, refreshToken, identityToken, refreshTime)
	return err
}

func (db *SQLite3) AddRole(tx *sql.Tx, user *User, role string) error {
	ui, ok := user.db.(userSQLite3)
	if !ok || ui.rowid <= 0 {
		return ErrInvalidUserID
	}

	r := tx.QueryRow("INSERT INTO roles (name) VALUES ($1) ON CONFLICT DO NOTHING RETURNING id;", role)
	if err := r.Err(); err != nil {
		return err
	}
	var roleid int64
	if err := r.Scan(&roleid); err != nil {
		return err
	}

	_, err := tx.Exec("INSERT INTO users_roles (userid, roleid) VALUES ($1, $2) ON CONFLICT DO NOTHING;", ui.rowid, roleid)
	return err
}

func (db *SQLite3) RemoveRole(tx *sql.Tx, user *User, role string) error {
	ui, ok := user.db.(userSQLite3)
	if !ok || ui.rowid <= 0 {
		return ErrInvalidUserID
	}

	r := tx.QueryRow("SELECT roleid FROM roles WHERE name = $1;", role)
	if err := r.Err(); err != nil {
		return err
	}
	var roleid int64
	if err := r.Scan(&roleid); err != nil {
		return err
	}

	_, err := tx.Exec("DELETE FROM users_roles WHERE userid = $1 AND roleid = $2;", ui.rowid, roleid)
	return err
}

func (db *SQLite3) QueryRoles(tx *sql.Tx, user *User) ([]string, error) {
	ui, ok := user.db.(userSQLite3)
	if !ok || ui.rowid <= 0 {
		return nil, ErrInvalidUserID
	}
	rs, err := tx.Query("SELECT roles.name FROM users_roles INNER JOIN roles ON users_roles.roleid = roles.id WHERE users_roles.userid = $1", ui.rowid)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var roles []string
	for rs.Next() {
		var role string
		if err = rs.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err = rs.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}
