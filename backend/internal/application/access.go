// Package application coordinates the platform's domain use cases and ports.
package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// RoleEmployee and the related constants define the supported access roles and account states.
const (
	RoleEmployee       Role = "employee"
	RoleAnalyticsAdmin Role = "analytics_admin"

	AccountPendingActivation AccountState = "pending_activation"
	AccountActive            AccountState = "active"
	AccountDeactivated       AccountState = "deactivated"
)

// ErrUnauthenticated and the related errors describe stable application outcomes.
var (
	ErrUnauthenticated      = errors.New("authentication required")
	ErrPasswordInvalid      = errors.New("password does not meet policy")
	ErrPasswordChangeNeeded = errors.New("password change required")
	ErrForbidden            = errors.New("operation is forbidden")
	ErrNotFound             = errors.New("resource not found")
	ErrConflict             = errors.New("resource state conflict")
	ErrValidation           = errors.New("request validation failed")
	ErrRateLimited          = errors.New("login attempts are rate limited")
	loginPattern            = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
)

// Role is the server-authoritative access role of a local account.
type Role string

// AccountState is the lifecycle state of a local account.
type AccountState string

// Account is a local employee identity persisted by the access repository.
type Account struct {
	ID                         string
	Login                      string
	DisplayName                string
	Role                       Role
	State                      AccountState
	PasswordHash               string
	TemporaryPasswordExpiresAt *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// BootstrapCredentials configures the first administrator for an empty database.
type BootstrapCredentials struct {
	Login       string
	Password    string
	DisplayName string
}

// Present reports whether any bootstrap setting was supplied.
func (credentials BootstrapCredentials) Present() bool {
	return credentials.Login != "" || credentials.Password != "" || credentials.DisplayName != ""
}

// NewAccount contains validated values used to persist a local account.
type NewAccount struct {
	Login                      string
	NormalizedLogin            string
	DisplayName                string
	Role                       Role
	State                      AccountState
	PasswordHash               string
	TemporaryPasswordExpiresAt time.Time
	CreatedAt                  time.Time
}

// NewSession contains hashed server-side session values ready for persistence.
type NewSession struct {
	AccountID     string
	TokenHash     string
	CreatedAt     time.Time
	LastSeenAt    time.Time
	ExpiresAt     time.Time
	IdleExpiresAt time.Time
}

// AuthenticatedSession combines the current account with its opaque cookie value.
type AuthenticatedSession struct {
	Account  Account
	RawToken string
}

// AnalyticalSession is the shared session metadata visible in the workspace.
type AnalyticalSession struct {
	ID                            string
	Name                          string
	Status                        string
	CreatedAt                     time.Time
	CreatedByID                   string
	CreatedByDisplayName          string
	Regions                       []string
	RegionCodes                   []string
	ResidentialComplexes          []string
	ResidentialComplexIDs         []string
	ResidentialComplexRegionCodes []string
	Segments                      []string
	SegmentCodes                  []string
	ObservationIDs                []string
	ObservedFrom                  *time.Time
	ObservedTo                    *time.Time
	PeriodFrom                    *time.Time
	PeriodTo                      *time.Time
	CollectionProgress            *CollectionProgress
	CurrentDashboard              *CurrentDashboard
}

// CurrentDashboard is the persisted current-only ЕИСЖС dashboard result.
type CurrentDashboard struct {
	SourceLabel        string
	CumulativeDDUCount *int64
}

// AnalyticalSessionPage is one cursor-addressed page of shared sessions.
type AnalyticalSessionPage struct {
	Items      []AnalyticalSession
	NextCursor string
}

// TemporaryCredential is a one-time response containing a generated password.
type TemporaryCredential struct {
	Account           Account
	TemporaryPassword string
	ExpiresAt         time.Time
}

// EmployeeChanges contains mutable employee account fields.
type EmployeeChanges struct {
	DisplayName *string
	Role        *Role
}

// AccessRepository persists accounts, sessions, throttles, and access audit events.
type AccessRepository interface {
	CountAccounts(context.Context) (int64, error)
	CreateBootstrapAdmin(context.Context, NewAccount) (bool, error)
	FindAccountByLogin(context.Context, string) (Account, bool, error)
	StartSession(context.Context, NewSession) error
	FindAccountBySession(context.Context, string, time.Time, time.Time) (Account, bool, error)
	ActivateAccount(context.Context, string, string, NewSession, time.Time) error
	RevokeSession(context.Context, string, time.Time) error
	LoginAllowed(context.Context, string, string, time.Time) (bool, time.Duration, error)
	RecordLoginFailure(context.Context, string, string, time.Time) error
	ClearLoginFailures(context.Context, string) error
	ListAnalyticalSessions(context.Context, int, int) ([]AnalyticalSession, bool, error)
	GetAnalyticalSession(context.Context, string) (AnalyticalSession, bool, error)
	ListAccounts(context.Context) ([]Account, error)
	CreateAccount(context.Context, string, NewAccount) (Account, error)
	UpdateAccount(context.Context, string, string, EmployeeChanges, time.Time) (Account, error)
	DeactivateAccount(context.Context, string, string, time.Time) (Account, error)
	IssueTemporaryPassword(context.Context, string, string, string, time.Time, time.Time, bool) (Account, error)
	RevokeAllSessions(context.Context, string, string, time.Time) (bool, error)
}

// CredentialManager owns password hashing and cryptographic token generation.
type CredentialManager interface {
	HashPassword(string) (string, error)
	VerifyPassword(string, string) (bool, error)
	ValidatePassword(string) error
	NewSessionToken() (string, string, error)
	HashSessionToken(string) string
	NewTemporaryPassword() (string, error)
}

// AccessService coordinates local authentication and authorization behavior.
type AccessService struct {
	repository  AccessRepository
	credentials CredentialManager
	now         func() time.Time
}

// NewAccessService creates an access service using the provided ports.
func NewAccessService(repository AccessRepository, credentials CredentialManager) *AccessService {
	return &AccessService{repository: repository, credentials: credentials, now: time.Now}
}

// EnsureBootstrap creates the first pending administrator only in an empty database.
func (service *AccessService) EnsureBootstrap(
	ctx context.Context,
	bootstrap BootstrapCredentials,
) error {
	count, err := service.repository.CountAccounts(ctx)
	if err != nil {
		return fmt.Errorf("count local accounts: %w", err)
	}
	if count > 0 {
		return nil
	}
	account, err := service.bootstrapAccount(bootstrap)
	if err != nil {
		return err
	}
	if _, err := service.repository.CreateBootstrapAdmin(ctx, account); err != nil {
		return fmt.Errorf("create bootstrap administrator: %w", err)
	}
	return nil
}

func (service *AccessService) bootstrapAccount(bootstrap BootstrapCredentials) (NewAccount, error) {
	login, err := normalizeLogin(bootstrap.Login)
	if err != nil || bootstrap.Password == "" {
		return NewAccount{}, errors.New("empty database requires valid bootstrap administrator credentials")
	}
	if err := service.credentials.ValidatePassword(bootstrap.Password); err != nil {
		return NewAccount{}, fmt.Errorf("validate bootstrap administrator password: %w", err)
	}
	passwordHash, err := service.credentials.HashPassword(bootstrap.Password)
	if err != nil {
		return NewAccount{}, fmt.Errorf("hash bootstrap administrator password: %w", err)
	}
	displayName := strings.TrimSpace(bootstrap.DisplayName)
	if displayName == "" {
		displayName = bootstrap.Login
	}
	now := service.now().UTC()
	return NewAccount{
		Login: bootstrap.Login, NormalizedLogin: login, DisplayName: displayName,
		Role: RoleAnalyticsAdmin, State: AccountPendingActivation, PasswordHash: passwordHash,
		TemporaryPasswordExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
	}, nil
}

// Login verifies local credentials and starts an independent server-side session.
func (service *AccessService) Login(
	ctx context.Context,
	login string,
	password string,
	clientIP string,
) (AuthenticatedSession, error) {
	now := service.now().UTC()
	normalized, err := service.normalizeLoginAttempt(ctx, login, clientIP, now)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	account, err := service.loginAccount(ctx, normalized, clientIP, now)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	if err := service.verifyLoginPassword(ctx, account, password, normalized, clientIP, now); err != nil {
		return AuthenticatedSession{}, err
	}
	if err := service.repository.ClearLoginFailures(ctx, normalized); err != nil {
		return AuthenticatedSession{}, fmt.Errorf("clear login throttle: %w", err)
	}
	session, err := service.newSession(account.ID, now)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	if err := service.repository.StartSession(ctx, session.record); err != nil {
		return AuthenticatedSession{}, fmt.Errorf("start server-side session: %w", err)
	}
	return AuthenticatedSession{Account: account, RawToken: session.rawToken}, nil
}

func (service *AccessService) normalizeLoginAttempt(
	ctx context.Context,
	login string,
	clientIP string,
	now time.Time,
) (string, error) {
	normalized, err := normalizeLogin(login)
	if err == nil {
		return normalized, nil
	}
	normalized = strings.ToLower(strings.TrimSpace(login))
	if recordErr := service.repository.RecordLoginFailure(ctx, normalized, clientIP, now); recordErr != nil {
		return "", fmt.Errorf("record invalid login attempt: %w", recordErr)
	}
	return "", ErrUnauthenticated
}

func (service *AccessService) loginAccount(
	ctx context.Context,
	normalized string,
	clientIP string,
	now time.Time,
) (Account, error) {
	allowed, _, err := service.repository.LoginAllowed(ctx, normalized, clientIP, now)
	if err != nil {
		return Account{}, fmt.Errorf("check login throttle: %w", err)
	}
	if !allowed {
		return Account{}, ErrRateLimited
	}
	account, found, err := service.repository.FindAccountByLogin(ctx, normalized)
	if err != nil {
		return Account{}, fmt.Errorf("find local account: %w", err)
	}
	if found && account.State != AccountDeactivated {
		return account, nil
	}
	return Account{}, service.failedLogin(ctx, normalized, clientIP, now)
}

func (service *AccessService) verifyLoginPassword(
	ctx context.Context,
	account Account,
	password string,
	normalized string,
	clientIP string,
	now time.Time,
) error {
	verified, err := service.credentials.VerifyPassword(password, account.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if verified && !temporaryPasswordExpired(account, now) {
		return nil
	}
	return service.failedLogin(ctx, normalized, clientIP, now)
}

func (service *AccessService) failedLogin(
	ctx context.Context,
	normalized string,
	clientIP string,
	now time.Time,
) error {
	if err := service.repository.RecordLoginFailure(ctx, normalized, clientIP, now); err != nil {
		return fmt.Errorf("record failed login: %w", err)
	}
	return ErrUnauthenticated
}

// CurrentAccount resolves and refreshes a valid server-side session.
func (service *AccessService) CurrentAccount(ctx context.Context, rawToken string) (Account, error) {
	if rawToken == "" {
		return Account{}, ErrUnauthenticated
	}
	now := service.now().UTC()
	account, found, err := service.repository.FindAccountBySession(
		ctx,
		service.credentials.HashSessionToken(rawToken),
		now,
		now.Add(2*time.Hour),
	)
	if err != nil {
		return Account{}, fmt.Errorf("find server-side session: %w", err)
	}
	if !found {
		return Account{}, ErrUnauthenticated
	}
	return account, nil
}

// ChangeTemporaryPassword activates a pending account and revokes its temporary sessions.
func (service *AccessService) ChangeTemporaryPassword(
	ctx context.Context,
	rawToken string,
	password string,
) (AuthenticatedSession, error) {
	account, err := service.CurrentAccount(ctx, rawToken)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	if account.State != AccountPendingActivation {
		return AuthenticatedSession{}, ErrUnauthenticated
	}
	if err := service.credentials.ValidatePassword(password); err != nil {
		return AuthenticatedSession{}, ErrPasswordInvalid
	}
	passwordHash, err := service.credentials.HashPassword(password)
	if err != nil {
		return AuthenticatedSession{}, fmt.Errorf("hash permanent password: %w", err)
	}
	now := service.now().UTC()
	session, err := service.newSession(account.ID, now)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	if err := service.repository.ActivateAccount(ctx, account.ID, passwordHash, session.record, now); err != nil {
		return AuthenticatedSession{}, fmt.Errorf("activate local account: %w", err)
	}
	account.State = AccountActive
	account.PasswordHash = ""
	account.TemporaryPasswordExpiresAt = nil
	return AuthenticatedSession{Account: account, RawToken: session.rawToken}, nil
}

// Logout revokes only the current server-side session.
func (service *AccessService) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if err := service.repository.RevokeSession(
		ctx,
		service.credentials.HashSessionToken(rawToken),
		service.now().UTC(),
	); err != nil {
		return fmt.Errorf("revoke server-side session: %w", err)
	}
	return nil
}

// ListAnalyticalSessions returns one ordered page of shared non-deleted sessions.
func (service *AccessService) ListAnalyticalSessions(
	ctx context.Context,
	rawToken string,
	cursor string,
	limit int,
) (AnalyticalSessionPage, error) {
	account, err := service.CurrentAccount(ctx, rawToken)
	if err != nil {
		return AnalyticalSessionPage{}, err
	}
	if account.State != AccountActive {
		return AnalyticalSessionPage{}, ErrPasswordChangeNeeded
	}
	offset, limit, err := pagination(cursor, limit)
	if err != nil {
		return AnalyticalSessionPage{}, ErrValidation
	}
	sessions, hasMore, err := service.repository.ListAnalyticalSessions(ctx, offset, limit)
	if err != nil {
		return AnalyticalSessionPage{}, fmt.Errorf("list analytical sessions: %w", err)
	}
	nextCursor := ""
	if hasMore {
		nextCursor = encodeCursor(offset + len(sessions))
	}
	return AnalyticalSessionPage{Items: sessions, NextCursor: nextCursor}, nil
}

func pagination(cursor string, limit int) (int, int, error) {
	offset, err := decodeCursor(cursor)
	if err != nil {
		return 0, 0, err
	}
	if limit == 0 {
		limit = 30
	}
	if limit < 1 || limit > 100 {
		return 0, 0, ErrValidation
	}
	return offset, limit, nil
}

// GetAnalyticalSession returns one shared non-deleted session.
func (service *AccessService) GetAnalyticalSession(
	ctx context.Context,
	rawToken string,
	sessionID string,
) (AnalyticalSession, error) {
	account, err := service.activeAccount(ctx, rawToken)
	if err != nil {
		return AnalyticalSession{}, err
	}
	_ = account
	session, found, err := service.repository.GetAnalyticalSession(ctx, sessionID)
	if err != nil {
		return AnalyticalSession{}, fmt.Errorf("get analytical session: %w", err)
	}
	if !found {
		return AnalyticalSession{}, ErrNotFound
	}
	return session, nil
}

// ListEmployees returns local accounts to an authenticated administrator.
func (service *AccessService) ListEmployees(ctx context.Context, rawToken string) ([]Account, error) {
	if _, err := service.administrator(ctx, rawToken); err != nil {
		return nil, err
	}
	accounts, err := service.repository.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list employee accounts: %w", err)
	}
	return accounts, nil
}

// CreateEmployee creates a pending account and returns its password exactly once.
func (service *AccessService) CreateEmployee(
	ctx context.Context,
	rawToken string,
	login string,
	displayName string,
	role Role,
) (TemporaryCredential, error) {
	actor, err := service.administrator(ctx, rawToken)
	if err != nil {
		return TemporaryCredential{}, err
	}
	normalized, err := normalizeLogin(login)
	if err != nil || !validDisplayName(displayName) || !validRole(role) {
		return TemporaryCredential{}, ErrValidation
	}
	password, passwordHash, expiresAt, err := service.temporaryPassword()
	if err != nil {
		return TemporaryCredential{}, err
	}
	now := service.now().UTC()
	account, err := service.repository.CreateAccount(ctx, actor.ID, NewAccount{
		Login: strings.TrimSpace(login), NormalizedLogin: normalized,
		DisplayName: strings.TrimSpace(displayName), Role: role,
		State: AccountPendingActivation, PasswordHash: passwordHash,
		TemporaryPasswordExpiresAt: expiresAt, CreatedAt: now,
	})
	if err != nil {
		return TemporaryCredential{}, fmt.Errorf("create employee account: %w", err)
	}
	return TemporaryCredential{Account: account, TemporaryPassword: password, ExpiresAt: expiresAt}, nil
}

// UpdateEmployee changes the display name or role of a local account.
func (service *AccessService) UpdateEmployee(
	ctx context.Context,
	rawToken string,
	employeeID string,
	changes EmployeeChanges,
) (Account, error) {
	actor, err := service.administrator(ctx, rawToken)
	if err != nil {
		return Account{}, err
	}
	if changes.DisplayName == nil && changes.Role == nil {
		return Account{}, ErrValidation
	}
	if changes.DisplayName != nil {
		trimmed := strings.TrimSpace(*changes.DisplayName)
		if !validDisplayName(trimmed) {
			return Account{}, ErrValidation
		}
		changes.DisplayName = &trimmed
	}
	if changes.Role != nil && !validRole(*changes.Role) {
		return Account{}, ErrValidation
	}
	account, err := service.repository.UpdateAccount(ctx, actor.ID, employeeID, changes, service.now().UTC())
	if err != nil {
		return Account{}, fmt.Errorf("update employee account: %w", err)
	}
	return account, nil
}

// DeactivateEmployee disables an account and revokes all of its sessions.
func (service *AccessService) DeactivateEmployee(
	ctx context.Context,
	rawToken string,
	employeeID string,
) (Account, error) {
	actor, err := service.administrator(ctx, rawToken)
	if err != nil {
		return Account{}, err
	}
	account, err := service.repository.DeactivateAccount(ctx, actor.ID, employeeID, service.now().UTC())
	if err != nil {
		return Account{}, fmt.Errorf("deactivate employee account: %w", err)
	}
	return account, nil
}

// RestoreEmployee returns a deactivated account to pending activation.
func (service *AccessService) RestoreEmployee(
	ctx context.Context,
	rawToken string,
	employeeID string,
) (TemporaryCredential, error) {
	return service.issueTemporaryPassword(ctx, rawToken, employeeID, true)
}

// ResetEmployeePassword issues a replacement temporary password.
func (service *AccessService) ResetEmployeePassword(
	ctx context.Context,
	rawToken string,
	employeeID string,
) (TemporaryCredential, error) {
	return service.issueTemporaryPassword(ctx, rawToken, employeeID, false)
}

// RevokeEmployeeSessions revokes every server-side session for an account.
func (service *AccessService) RevokeEmployeeSessions(
	ctx context.Context,
	rawToken string,
	employeeID string,
) error {
	actor, err := service.administrator(ctx, rawToken)
	if err != nil {
		return err
	}
	found, err := service.repository.RevokeAllSessions(ctx, actor.ID, employeeID, service.now().UTC())
	if err != nil {
		return fmt.Errorf("revoke employee sessions: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

type sessionToken struct {
	rawToken string
	record   NewSession
}

func (service *AccessService) newSession(accountID string, now time.Time) (sessionToken, error) {
	rawToken, tokenHash, err := service.credentials.NewSessionToken()
	if err != nil {
		return sessionToken{}, fmt.Errorf("generate server-side session token: %w", err)
	}
	return sessionToken{rawToken: rawToken, record: NewSession{
		AccountID: accountID, TokenHash: tokenHash, CreatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(12 * time.Hour), IdleExpiresAt: now.Add(2 * time.Hour),
	}}, nil
}

func normalizeLogin(login string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(login))
	if !loginPattern.MatchString(normalized) {
		return "", errors.New("login must be 3 to 64 ASCII letters, digits, dots, underscores, or hyphens")
	}
	return normalized, nil
}

func temporaryPasswordExpired(account Account, now time.Time) bool {
	return account.State == AccountPendingActivation &&
		(account.TemporaryPasswordExpiresAt == nil || !account.TemporaryPasswordExpiresAt.After(now))
}

func (service *AccessService) activeAccount(ctx context.Context, rawToken string) (Account, error) {
	account, err := service.CurrentAccount(ctx, rawToken)
	if err != nil {
		return Account{}, err
	}
	if account.State != AccountActive {
		return Account{}, ErrPasswordChangeNeeded
	}
	return account, nil
}

func (service *AccessService) administrator(ctx context.Context, rawToken string) (Account, error) {
	account, err := service.activeAccount(ctx, rawToken)
	if err != nil {
		return Account{}, err
	}
	if account.Role != RoleAnalyticsAdmin {
		return Account{}, ErrForbidden
	}
	return account, nil
}

func (service *AccessService) temporaryPassword() (string, string, time.Time, error) {
	password, err := service.credentials.NewTemporaryPassword()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("generate temporary password: %w", err)
	}
	passwordHash, err := service.credentials.HashPassword(password)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("hash temporary password: %w", err)
	}
	return password, passwordHash, service.now().UTC().Add(24 * time.Hour), nil
}

func (service *AccessService) issueTemporaryPassword(
	ctx context.Context,
	rawToken string,
	employeeID string,
	restore bool,
) (TemporaryCredential, error) {
	actor, err := service.administrator(ctx, rawToken)
	if err != nil {
		return TemporaryCredential{}, err
	}
	password, passwordHash, expiresAt, err := service.temporaryPassword()
	if err != nil {
		return TemporaryCredential{}, err
	}
	action := "reset_password"
	if restore {
		action = "restore"
	}
	account, err := service.repository.IssueTemporaryPassword(
		ctx, actor.ID, employeeID, passwordHash, expiresAt, service.now().UTC(), restore,
	)
	if err != nil {
		return TemporaryCredential{}, fmt.Errorf("%s employee account: %w", action, err)
	}
	return TemporaryCredential{Account: account, TemporaryPassword: password, ExpiresAt: expiresAt}, nil
}

func validRole(role Role) bool {
	return role == RoleEmployee || role == RoleAnalyticsAdmin
}

func validDisplayName(name string) bool {
	length := utf8.RuneCountInString(strings.TrimSpace(name))
	return length >= 1 && length <= 120
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid analytical session cursor")
	}
	return offset, nil
}

// DeterministicSessionName derives a stable compact title from canonical filters.
func DeterministicSessionName(session AnalyticalSession) string {
	if len(session.Regions) == 1 && len(session.ResidentialComplexes) == 1 {
		return session.Regions[0] + " - " + session.ResidentialComplexes[0]
	}
	if len(session.Regions) == 1 {
		return fmt.Sprintf("%s - %d ЖК", session.Regions[0], len(session.ResidentialComplexes))
	}
	return fmt.Sprintf("%d регионов - %d ЖК", len(session.Regions), len(session.ResidentialComplexes))
}

// AnalyticalSessionSubtitle summarizes segments and the analysis period.
func AnalyticalSessionSubtitle(session AnalyticalSession) string {
	subtitle := strings.Join(session.Segments, ", ")
	if session.PeriodFrom == nil || session.PeriodTo == nil {
		return subtitle
	}
	return subtitle + " · " + session.PeriodFrom.Format("02.01.2006") +
		" - " + session.PeriodTo.Format("02.01.2006")
}
