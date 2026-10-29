package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	GrantStatic = "static"
	GrantOAuth  = "oauth"
)

const (
	ApprovalElicitation = "elicitation"
	ApprovalApp         = "app"
)

func ValidApproval(mode string) bool {
	return mode == ApprovalElicitation || mode == ApprovalApp
}

type Grant struct {
	ID        int64
	ClientID  string
	Name      string
	Kind      string
	ReadOnly  bool
	Approval  string
	CreatedAt time.Time
	RevokedAt time.Time
}

func (g Grant) Revoked() bool { return !g.RevokedAt.IsZero() }

func (g Grant) TrustsApp() bool { return g.Approval == ApprovalApp }

type NewGrant struct {
	ClientID            string
	Name                string
	Kind                string
	ReadOnly            bool
	Approval            string
	AllExistingAccounts bool
	AccountNames        []string
	CreatedAt           time.Time
	Tokens              []Token
}

const (
	grantColumns         = `id, client_id, name, kind, read_only, approval, created_at, revoked_at`
	prefixedGrantColumns = `g.id, g.client_id, g.name, g.kind, g.read_only, g.approval, g.created_at, g.revoked_at`
)

func (s *Store) CreateGrant(ctx context.Context, n NewGrant) (Grant, error) {
	var grant Grant
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		grant, err = createGrant(ctx, tx, n)
		return err
	})
	return grant, err
}

func createGrant(ctx context.Context, tx *sql.Tx, n NewGrant) (Grant, error) {
	if err := ensureNameFree(ctx, tx, n.Name); err != nil {
		return Grant{}, err
	}
	accountIDs, err := grantedAccountIDs(ctx, tx, n)
	if err != nil {
		return Grant{}, err
	}
	id, err := insertGrant(ctx, tx, n)
	if err != nil {
		return Grant{}, err
	}
	if err := linkAccounts(ctx, tx, id, accountIDs); err != nil {
		return Grant{}, err
	}
	for _, token := range n.Tokens {
		token.GrantID = id
		if err := insertToken(ctx, tx, token); err != nil {
			return Grant{}, err
		}
	}
	return grantWhere(ctx, tx, `id = ?`, id)
}

func (s *Store) GrantByName(ctx context.Context, name string) (Grant, error) {
	return grantWhere(ctx, s.db, `name = ? AND revoked_at IS NULL`, name)
}

func (s *Store) Grant(ctx context.Context, id int64) (Grant, error) {
	return grantWhere(ctx, s.db, `id = ?`, id)
}

func (s *Store) ListGrants(ctx context.Context) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM grants WHERE revoked_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []Grant{}
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func (s *Store) RevokeGrant(ctx context.Context, name string, at time.Time) (Grant, error) {
	var grant Grant
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		active, err := grantWhere(ctx, tx, `name = ? AND revoked_at IS NULL`, name)
		if err != nil {
			return err
		}
		grant, err = revokeGrant(ctx, tx, active.ID, at)
		return err
	})
	return grant, err
}

func revokeGrant(ctx context.Context, tx *sql.Tx, id int64, at time.Time) (Grant, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE grants SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, millis(at), id); err != nil {
		return Grant{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tokens WHERE grant_id = ?`, id); err != nil {
		return Grant{}, err
	}
	return grantWhere(ctx, tx, `id = ?`, id)
}

func (s *Store) ActiveGrant(ctx context.Context, id int64) (Grant, error) {
	return grantWhere(ctx, s.db, `id = ? AND revoked_at IS NULL`, id)
}

func (s *Store) GrantAccounts(ctx context.Context, g Grant) ([]Account, error) {
	return queryAccounts(ctx, s.db, `SELECT `+accountColumns+` FROM accounts a JOIN grant_accounts ga ON ga.account_id = a.id WHERE ga.grant_id = ? ORDER BY a.name`, g.ID)
}

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func grantWhere(ctx context.Context, q rowQueryer, where string, args ...any) (Grant, error) {
	grant, err := scanGrant(q.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM grants WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	return grant, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanGrant(row scanner) (Grant, error) {
	return scanGrantWith(row)
}

func scanGrantWith(row scanner, extra ...any) (Grant, error) {
	var (
		g         Grant
		clientID  sql.NullString
		createdAt int64
		revokedAt sql.NullInt64
	)
	if err := row.Scan(append([]any{&g.ID, &clientID, &g.Name, &g.Kind, &g.ReadOnly, &g.Approval, &createdAt, &revokedAt}, extra...)...); err != nil {
		return Grant{}, err
	}
	g.ClientID = clientID.String
	g.CreatedAt = fromMillis(createdAt)
	g.RevokedAt = optionalTime(revokedAt)
	return g, nil
}

type UnknownAccountError struct {
	Name string
}

func (e *UnknownAccountError) Error() string { return "unknown account: " + e.Name }

func (e *UnknownAccountError) Is(target error) bool { return target == ErrUnknownAccount }

func ensureNameFree(ctx context.Context, tx *sql.Tx, name string) error {
	_, err := grantWhere(ctx, tx, `name = ? AND revoked_at IS NULL`, name)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrNameInUse, name)
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func grantedAccountIDs(ctx context.Context, tx *sql.Tx, n NewGrant) ([]int64, error) {
	return chosenAccountIDs(ctx, tx, n.AllExistingAccounts, n.AccountNames)
}

func chosenAccountIDs(ctx context.Context, tx *sql.Tx, allExisting bool, names []string) ([]int64, error) {
	if allExisting {
		return allAccountIDs(ctx, tx)
	}
	return accountIDsByName(ctx, tx, names)
}

func allAccountIDs(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func accountIDsByName(ctx context.Context, tx *sql.Tx, names []string) ([]int64, error) {
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE name = ?`, name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &UnknownAccountError{Name: name}
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func insertGrant(ctx context.Context, tx *sql.Tx, n NewGrant) (int64, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO grants (client_id, name, kind, read_only, approval, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		optionalText(n.ClientID), n.Name, n.Kind, n.ReadOnly, approvalOrDefault(n.Approval), millis(n.CreatedAt))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func linkAccounts(ctx context.Context, tx *sql.Tx, grantID int64, accountIDs []int64) error {
	for _, accountID := range accountIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO grant_accounts (grant_id, account_id) VALUES (?, ?)`, grantID, accountID); err != nil {
			return err
		}
	}
	return nil
}

func approvalOrDefault(mode string) string {
	if mode == "" {
		return ApprovalElicitation
	}
	return mode
}

type GrantChange struct {
	Name                string
	ChangeAccounts      bool
	AllExistingAccounts bool
	AccountNames        []string
	Approval            string
}

func (s *Store) ChangeGrant(ctx context.Context, c GrantChange) (Grant, error) {
	var grant Grant
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		active, err := grantWhere(ctx, tx, `name = ? AND revoked_at IS NULL`, c.Name)
		if err != nil {
			return err
		}
		if c.ChangeAccounts {
			if err := replaceAccounts(ctx, tx, active.ID, c); err != nil {
				return err
			}
		}
		if c.Approval != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE grants SET approval = ? WHERE id = ?`, c.Approval, active.ID); err != nil {
				return err
			}
		}
		grant, err = grantWhere(ctx, tx, `id = ?`, active.ID)
		return err
	})
	return grant, err
}

func replaceAccounts(ctx context.Context, tx *sql.Tx, grantID int64, c GrantChange) error {
	accountIDs, err := grantedAccountIDs(ctx, tx, NewGrant{AllExistingAccounts: c.AllExistingAccounts, AccountNames: c.AccountNames})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM grant_accounts WHERE grant_id = ?`, grantID); err != nil {
		return err
	}
	return linkAccounts(ctx, tx, grantID, accountIDs)
}
