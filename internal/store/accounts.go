package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	AccountPairing      = "pairing"
	AccountConnected    = "connected"
	AccountDisconnected = "disconnected"
	AccountLoggedOut    = "logged_out"
)

type Account struct {
	ID              int64
	Name            string
	JID             string
	Phone           string
	ProfileName     string
	Description     string
	RetentionMonths int
	State           string
	CreatedAt       time.Time
}

type NewAccount struct {
	Name        string
	Phone       string
	Description string
	CreatedAt   time.Time
}

type PhoneInUseError struct {
	Phone   string
	Account string
}

func (e *PhoneInUseError) Error() string {
	return fmt.Sprintf("the phone %s belongs to the account %s", e.Phone, e.Account)
}

const accountColumns = `a.id, a.name, a.jid, a.phone, a.profile_name, a.description, a.retention_months, a.state, a.created_at`

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	return queryAccounts(ctx, s.db, `SELECT `+accountColumns+` FROM accounts a ORDER BY a.name`)
}

func (s *Store) AccountByName(ctx context.Context, name string) (Account, error) {
	return accountWhere(ctx, s.db, `a.name = ?`, name)
}

func (s *Store) Account(ctx context.Context, id int64) (Account, error) {
	return accountWhere(ctx, s.db, `a.id = ?`, id)
}

func (s *Store) CreateAccount(ctx context.Context, n NewAccount) (Account, error) {
	var account Account
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if err := ensureAccountFree(ctx, tx, n); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO accounts (name, phone, description, state, created_at) VALUES (?, ?, ?, ?, ?)`,
			n.Name, n.Phone, optionalText(n.Description), AccountPairing, millis(n.CreatedAt))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		account, err = accountWhere(ctx, tx, `a.id = ?`, id)
		return err
	})
	return account, err
}

func ensureAccountFree(ctx context.Context, tx *sql.Tx, n NewAccount) error {
	if _, err := accountWhere(ctx, tx, `a.name = ?`, n.Name); !errors.Is(err, ErrNotFound) {
		if err == nil {
			return fmt.Errorf("%w: %s", ErrNameInUse, n.Name)
		}
		return err
	}
	owner, err := accountWhere(ctx, tx, `a.phone = ?`, n.Phone)
	if err == nil {
		return &PhoneInUseError{Phone: n.Phone, Account: owner.Name}
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (s *Store) SetAccountState(ctx context.Context, id int64, state string, at time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		return changeAccountState(ctx, tx, id, state, at)
	})
}

func (s *Store) SetAccountPaired(ctx context.Context, id int64, jid, phone string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET jid = ?, phone = ? WHERE id = ?`, jid, phone, id)
	return err
}

func (s *Store) SetAccountLoggedOut(ctx context.Context, id int64, at time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET jid = NULL WHERE id = ?`, id); err != nil {
			return err
		}
		return changeAccountState(ctx, tx, id, AccountLoggedOut, at)
	})
}

func changeAccountState(ctx context.Context, tx *sql.Tx, id int64, state string, at time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE accounts SET state = ? WHERE id = ? AND state <> ?`, state, id, state)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return err
	}
	return journalState(ctx, tx, id, state, at)
}

func (s *Store) SetAccountProfileName(ctx context.Context, id int64, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET profile_name = ? WHERE id = ?`, optionalText(name), id)
	return err
}

func (s *Store) SetAccountDescription(ctx context.Context, name, description string) (Account, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE accounts SET description = ? WHERE name = ?`, optionalText(description), name)
	if err != nil {
		return Account{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return Account{}, errors.Join(ErrNotFound, err)
	}
	return s.AccountByName(ctx, name)
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteUnfinishedPairings(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE state = ? AND jid IS NULL`, AccountPairing)
	return err
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func accountWhere(ctx context.Context, q rowQueryer, where string, args ...any) (Account, error) {
	account, err := scanAccount(q.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts a WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return account, err
}

func queryAccounts(ctx context.Context, q queryer, query string, args ...any) ([]Account, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []Account{}
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func scanAccount(row scanner) (Account, error) {
	var (
		a           Account
		jid         sql.NullString
		profileName sql.NullString
		description sql.NullString
		retention   sql.NullInt64
		createdAt   int64
	)
	err := row.Scan(&a.ID, &a.Name, &jid, &a.Phone, &profileName, &description, &retention, &a.State, &createdAt)
	a.JID = jid.String
	a.ProfileName = profileName.String
	a.Description = description.String
	a.RetentionMonths = int(retention.Int64)
	a.CreatedAt = fromMillis(createdAt)
	return a, err
}

func optionalText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
