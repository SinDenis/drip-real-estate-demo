package application_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SinDenis/drip-real-estate-demo/backend/internal/application"
)

func TestAccessServiceSupportsTheLocalAccountLifecycle(t *testing.T) {
	t.Parallel()
	fixture := newAccessFixture()
	bootstrapAndActivateAdministrator(t, fixture)
	verifySharedSessions(t, fixture)
	verifyEmployeeAdministration(t, fixture)
}

func TestAccessServiceRejectsInvalidOrUnauthorizedRequests(t *testing.T) {
	t.Parallel()
	fixture := newAccessFixture()
	if err := fixture.service.EnsureBootstrap(t.Context(), application.BootstrapCredentials{}); err == nil {
		t.Fatal("EnsureBootstrap(empty) succeeded")
	}
	bootstrapAndActivateAdministrator(t, fixture)
	assertAccessError(t, application.ErrUnauthenticated, func() error {
		_, err := fixture.service.Login(t.Context(), "!", "wrong", "127.0.0.1")
		return err
	})
	assertAccessError(t, application.ErrUnauthenticated, func() error {
		_, err := fixture.service.CurrentAccount(t.Context(), "")
		return err
	})
	assertInvalidAdministration(t, fixture)
}

func TestBootstrapCredentialsReportWhetherConfigurationWasSupplied(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		credentials application.BootstrapCredentials
		present     bool
	}{
		{name: "empty"},
		{name: "login", credentials: application.BootstrapCredentials{Login: "admin"}, present: true},
		{name: "password", credentials: application.BootstrapCredentials{Password: "secret"}, present: true},
		{name: "display name", credentials: application.BootstrapCredentials{DisplayName: "Admin"}, present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := test.credentials.Present(); actual != test.present {
				t.Fatalf("Present() = %t, want %t", actual, test.present)
			}
		})
	}
}

//nolint:funlen,gocyclo,gocognit // The table documents security-relevant public outcomes in one place.
func TestAccessServiceEnforcesAccountAndWorkspaceRules(t *testing.T) {
	t.Parallel()

	t.Run("bootstrap display name defaults to the configured login", func(t *testing.T) {
		fixture := newAccessFixture()
		err := fixture.service.EnsureBootstrap(t.Context(), application.BootstrapCredentials{
			Login: "Admin", Password: "Reliable bootstrap password",
		})
		if err != nil {
			t.Fatalf("EnsureBootstrap: %v", err)
		}
		if actual := fixture.repository.accounts["admin-id"].DisplayName; actual != "Admin" {
			t.Fatalf("display name = %q, want configured login", actual)
		}
	})

	t.Run("invalid bootstrap password fails before persistence", func(t *testing.T) {
		fixture := newAccessFixture()
		assertAccessError(t, application.ErrPasswordInvalid, func() error {
			return fixture.service.EnsureBootstrap(t.Context(), application.BootstrapCredentials{
				Login: "admin", Password: "short",
			})
		})
	})

	t.Run("blocked login returns the rate limit outcome", func(t *testing.T) {
		fixture := activeAccessFixture(t)
		fixture.repository.loginBlocked = true
		assertAccessError(t, application.ErrRateLimited, func() error {
			_, err := fixture.service.Login(
				t.Context(), "admin", "Reliable permanent password", "127.0.0.1",
			)
			return err
		})
	})

	t.Run("unknown wrong deactivated and expired accounts are unauthenticated", func(t *testing.T) {
		for _, scenario := range []struct {
			name     string
			prepare  func(*accessFixture)
			login    string
			password string
		}{
			{name: "unknown", login: "unknown", password: "wrong"},
			{name: "wrong password", login: "admin", password: "wrong"},
			{
				name: "deactivated", login: "admin", password: "Reliable permanent password",
				prepare: func(fixture *accessFixture) {
					account := fixture.repository.accounts["admin-id"]
					account.State = application.AccountDeactivated
					fixture.repository.accounts["admin-id"] = account
				},
			},
			{
				name: "expired temporary password", login: "admin", password: "Reliable permanent password",
				prepare: func(fixture *accessFixture) {
					account := fixture.repository.accounts["admin-id"]
					account.State = application.AccountPendingActivation
					expired := time.Now().Add(-time.Hour)
					account.TemporaryPasswordExpiresAt = &expired
					fixture.repository.accounts["admin-id"] = account
				},
			},
		} {
			t.Run(scenario.name, func(t *testing.T) {
				fixture := activeAccessFixture(t)
				if scenario.prepare != nil {
					scenario.prepare(fixture)
				}
				assertAccessError(t, application.ErrUnauthenticated, func() error {
					_, err := fixture.service.Login(
						t.Context(), scenario.login, scenario.password, "127.0.0.1",
					)
					return err
				})
			})
		}
	})

	t.Run("pending account can only change its password", func(t *testing.T) {
		fixture := newAccessFixture()
		bootstrap := application.BootstrapCredentials{
			Login: "admin", Password: "Reliable bootstrap password",
		}
		if err := fixture.service.EnsureBootstrap(t.Context(), bootstrap); err != nil {
			t.Fatalf("EnsureBootstrap: %v", err)
		}
		login, err := fixture.service.Login(t.Context(), "admin", bootstrap.Password, "127.0.0.1")
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		assertAccessError(t, application.ErrPasswordChangeNeeded, func() error {
			_, err := fixture.service.ListAnalyticalSessions(t.Context(), login.RawToken, "", 0)
			return err
		})
		assertAccessError(t, application.ErrPasswordInvalid, func() error {
			_, err := fixture.service.ChangeTemporaryPassword(t.Context(), login.RawToken, "short")
			return err
		})
	})

	t.Run("active account cannot repeat temporary password activation", func(t *testing.T) {
		fixture := activeAccessFixture(t)
		assertAccessError(t, application.ErrUnauthenticated, func() error {
			_, err := fixture.service.ChangeTemporaryPassword(
				t.Context(), fixture.adminToken, "Another reliable permanent password",
			)
			return err
		})
	})

	t.Run("employee cannot use administrator operations", func(t *testing.T) {
		fixture := activeAccessFixture(t)
		account := fixture.repository.accounts["admin-id"]
		account.Role = application.RoleEmployee
		fixture.repository.accounts["admin-id"] = account
		assertAccessError(t, application.ErrForbidden, func() error {
			_, err := fixture.service.ListEmployees(t.Context(), fixture.adminToken)
			return err
		})
	})

	t.Run("unknown session token is unauthenticated", func(t *testing.T) {
		fixture := activeAccessFixture(t)
		assertAccessError(t, application.ErrUnauthenticated, func() error {
			_, err := fixture.service.CurrentAccount(t.Context(), "unknown-token")
			return err
		})
	})

	t.Run("default pagination and deterministic names are stable", func(t *testing.T) {
		fixture := activeAccessFixture(t)
		page, err := fixture.service.ListAnalyticalSessions(t.Context(), fixture.adminToken, "", 0)
		if err != nil || len(page.Items) != 2 || page.NextCursor != "" {
			t.Fatalf("default page = %#v, %v", page, err)
		}
		if name := application.DeterministicSessionName(analyticalSession(
			"one-region", []string{"Москва"}, []string{"ЖК 1", "ЖК 2"},
		)); name != "Москва - 2 ЖК" {
			t.Fatalf("one-region name = %q", name)
		}
		if name := application.DeterministicSessionName(analyticalSession(
			"many-regions", []string{"Москва", "Тула"}, []string{"ЖК 1", "ЖК 2"},
		)); name != "2 регионов - 2 ЖК" {
			t.Fatalf("multi-region name = %q", name)
		}
	})
}

//nolint:funlen // Every case crosses the public service boundary and verifies contextual error propagation.
func TestAccessServicePropagatesPortFailures(t *testing.T) {
	t.Parallel()
	injected := errors.New("injected port failure")

	for _, test := range []struct {
		name       string
		repository string
		credential string
		operation  func(*accessFixture) error
	}{
		{
			name: "check login throttle", repository: "LoginAllowed",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "Reliable permanent password", "ip")
				return err
			},
		},
		{
			name: "find account", repository: "FindAccountByLogin",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "Reliable permanent password", "ip")
				return err
			},
		},
		{
			name: "verify password", credential: "VerifyPassword",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "Reliable permanent password", "ip")
				return err
			},
		},
		{
			name: "record failed login", repository: "RecordLoginFailure",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "wrong", "ip")
				return err
			},
		},
		{
			name: "clear failed logins", repository: "ClearLoginFailures",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "Reliable permanent password", "ip")
				return err
			},
		},
		{
			name: "generate login session", credential: "NewSessionToken",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "Reliable permanent password", "ip")
				return err
			},
		},
		{
			name: "persist login session", repository: "StartSession",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.Login(t.Context(), "admin", "Reliable permanent password", "ip")
				return err
			},
		},
		{
			name: "find current session", repository: "FindAccountBySession",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.CurrentAccount(t.Context(), fixture.adminToken)
				return err
			},
		},
		{
			name: "revoke current session", repository: "RevokeSession",
			operation: func(fixture *accessFixture) error {
				return fixture.service.Logout(t.Context(), fixture.adminToken)
			},
		},
		{
			name: "list analytical sessions", repository: "ListAnalyticalSessions",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.ListAnalyticalSessions(t.Context(), fixture.adminToken, "", 30)
				return err
			},
		},
		{
			name: "get analytical session", repository: "GetAnalyticalSession",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.GetAnalyticalSession(t.Context(), fixture.adminToken, "session-1")
				return err
			},
		},
		{
			name: "list employee accounts", repository: "ListAccounts",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.ListEmployees(t.Context(), fixture.adminToken)
				return err
			},
		},
		{
			name: "create employee account", repository: "CreateAccount",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.CreateEmployee(
					t.Context(), fixture.adminToken, "employee", "Employee", application.RoleEmployee,
				)
				return err
			},
		},
		{
			name: "update employee account", repository: "UpdateAccount",
			operation: func(fixture *accessFixture) error {
				name := "Updated"
				_, err := fixture.service.UpdateEmployee(
					t.Context(), fixture.adminToken, "employee-id",
					application.EmployeeChanges{DisplayName: &name},
				)
				return err
			},
		},
		{
			name: "deactivate employee account", repository: "DeactivateAccount",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.DeactivateEmployee(t.Context(), fixture.adminToken, "employee-id")
				return err
			},
		},
		{
			name: "restore employee account", repository: "IssueTemporaryPassword",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.RestoreEmployee(t.Context(), fixture.adminToken, "employee-id")
				return err
			},
		},
		{
			name: "revoke employee sessions", repository: "RevokeAllSessions",
			operation: func(fixture *accessFixture) error {
				return fixture.service.RevokeEmployeeSessions(t.Context(), fixture.adminToken, "employee-id")
			},
		},
		{
			name: "generate temporary password", credential: "NewTemporaryPassword",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.ResetEmployeePassword(t.Context(), fixture.adminToken, "employee-id")
				return err
			},
		},
		{
			name: "hash temporary password", credential: "HashPassword",
			operation: func(fixture *accessFixture) error {
				_, err := fixture.service.ResetEmployeePassword(t.Context(), fixture.adminToken, "employee-id")
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := activeAccessFixture(t)
			if test.repository != "" {
				fixture.repository.failures[test.repository] = injected
			}
			if test.credential != "" {
				fixture.credentials.failures[test.credential] = injected
			}
			if err := test.operation(fixture); !errors.Is(err, injected) {
				t.Fatalf("error = %v, want wrapped injected failure", err)
			}
		})
	}
}

func activeAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	fixture := newAccessFixture()
	bootstrapAndActivateAdministrator(t, fixture)
	return fixture
}

func bootstrapAndActivateAdministrator(t *testing.T, fixture *accessFixture) {
	t.Helper()
	bootstrap := application.BootstrapCredentials{
		Login: "Admin", Password: "Reliable bootstrap password", DisplayName: " Administrator ",
	}
	if err := fixture.service.EnsureBootstrap(t.Context(), bootstrap); err != nil {
		t.Fatalf("EnsureBootstrap: %v", err)
	}
	if err := fixture.service.EnsureBootstrap(t.Context(), application.BootstrapCredentials{}); err != nil {
		t.Fatalf("EnsureBootstrap(existing): %v", err)
	}
	login, err := fixture.service.Login(t.Context(), " ADMIN ", bootstrap.Password, "127.0.0.1")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	active, err := fixture.service.ChangeTemporaryPassword(
		t.Context(), login.RawToken, "Reliable permanent password",
	)
	if err != nil {
		t.Fatalf("ChangeTemporaryPassword: %v", err)
	}
	fixture.adminToken = active.RawToken
}

func verifySharedSessions(t *testing.T, fixture *accessFixture) {
	t.Helper()
	page, err := fixture.service.ListAnalyticalSessions(t.Context(), fixture.adminToken, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("ListAnalyticalSessions(first) = %#v, %v", page, err)
	}
	page, err = fixture.service.ListAnalyticalSessions(t.Context(), fixture.adminToken, page.NextCursor, 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor != "" {
		t.Fatalf("ListAnalyticalSessions(next) = %#v, %v", page, err)
	}
	session, err := fixture.service.GetAnalyticalSession(t.Context(), fixture.adminToken, "session-1")
	if err != nil || session.ID != "session-1" {
		t.Fatalf("GetAnalyticalSession = %#v, %v", session, err)
	}
	if application.DeterministicSessionName(session) != "Москва - ЖК Северный" {
		t.Fatalf("unexpected deterministic name: %q", application.DeterministicSessionName(session))
	}
	if application.AnalyticalSessionSubtitle(session) == "" {
		t.Fatal("empty analytical session subtitle")
	}
}

//nolint:funlen,gocyclo // One vertical workflow proves the one-time credential lifecycle end to end.
func verifyEmployeeAdministration(t *testing.T, fixture *accessFixture) {
	t.Helper()
	credential, err := fixture.service.CreateEmployee(
		t.Context(), fixture.adminToken, " Ivan.Petrov ", " Иван Петров ", application.RoleEmployee,
	)
	if err != nil || credential.TemporaryPassword == "" {
		t.Fatalf("CreateEmployee = %#v, %v", credential, err)
	}
	employeeID := credential.Account.ID
	name := "Иван Петров-младший"
	role := application.RoleAnalyticsAdmin
	updated, err := fixture.service.UpdateEmployee(
		t.Context(), fixture.adminToken, employeeID,
		application.EmployeeChanges{DisplayName: &name, Role: &role},
	)
	if err != nil || updated.DisplayName != name || updated.Role != role {
		t.Fatalf("UpdateEmployee = %#v, %v", updated, err)
	}
	if _, err := fixture.service.ListEmployees(t.Context(), fixture.adminToken); err != nil {
		t.Fatalf("ListEmployees: %v", err)
	}
	if _, err := fixture.service.DeactivateEmployee(t.Context(), fixture.adminToken, employeeID); err != nil {
		t.Fatalf("DeactivateEmployee: %v", err)
	}
	if _, err := fixture.service.RestoreEmployee(t.Context(), fixture.adminToken, employeeID); err != nil {
		t.Fatalf("RestoreEmployee: %v", err)
	}
	if _, err := fixture.service.ResetEmployeePassword(t.Context(), fixture.adminToken, employeeID); err != nil {
		t.Fatalf("ResetEmployeePassword: %v", err)
	}
	if err := fixture.service.RevokeEmployeeSessions(t.Context(), fixture.adminToken, employeeID); err != nil {
		t.Fatalf("RevokeEmployeeSessions: %v", err)
	}
	if err := fixture.service.Logout(t.Context(), fixture.adminToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if err := fixture.service.Logout(t.Context(), ""); err != nil {
		t.Fatalf("Logout(empty): %v", err)
	}
}

func assertInvalidAdministration(t *testing.T, fixture *accessFixture) {
	t.Helper()
	assertAccessError(t, application.ErrValidation, func() error {
		_, err := fixture.service.CreateEmployee(
			t.Context(), fixture.adminToken, "!", "", application.Role("owner"),
		)
		return err
	})
	assertAccessError(t, application.ErrValidation, func() error {
		_, err := fixture.service.UpdateEmployee(
			t.Context(), fixture.adminToken, "missing", application.EmployeeChanges{},
		)
		return err
	})
	assertAccessError(t, application.ErrValidation, func() error {
		_, err := fixture.service.ListAnalyticalSessions(t.Context(), fixture.adminToken, "invalid", 101)
		return err
	})
	assertAccessError(t, application.ErrNotFound, func() error {
		_, err := fixture.service.GetAnalyticalSession(t.Context(), fixture.adminToken, "missing")
		return err
	})
	assertAccessError(t, application.ErrNotFound, func() error {
		return fixture.service.RevokeEmployeeSessions(t.Context(), fixture.adminToken, "missing")
	})
}

func assertAccessError(t *testing.T, expected error, operation func() error) {
	t.Helper()
	if err := operation(); !errors.Is(err, expected) {
		t.Errorf("error = %v, want %v", err, expected)
	}
}

type accessFixture struct {
	repository  *memoryAccessRepository
	credentials *memoryCredentials
	service     *application.AccessService
	adminToken  string
}

func newAccessFixture() *accessFixture {
	repository := &memoryAccessRepository{
		accounts: map[string]application.Account{},
		sessions: map[string]string{},
		failures: map[string]error{},
		analyticalSessions: []application.AnalyticalSession{
			analyticalSession("session-1", []string{"Москва"}, []string{"ЖК Северный"}),
			analyticalSession("session-2", []string{"Москва", "Тула"}, []string{"ЖК Южный"}),
		},
	}
	credentials := &memoryCredentials{failures: map[string]error{}}
	return &accessFixture{
		repository:  repository,
		credentials: credentials,
		service:     application.NewAccessService(repository, credentials),
	}
}

type memoryCredentials struct {
	sequence int
	failures map[string]error
}

func (credentials *memoryCredentials) ValidatePassword(password string) error {
	if err := credentials.failures["ValidatePassword"]; err != nil {
		return err
	}
	if len(password) < 15 {
		return application.ErrPasswordInvalid
	}
	return nil
}

func (credentials *memoryCredentials) HashPassword(password string) (string, error) {
	if err := credentials.failures["HashPassword"]; err != nil {
		return "", err
	}
	return "hash:" + password, nil
}

func (credentials *memoryCredentials) VerifyPassword(password string, hash string) (bool, error) {
	if err := credentials.failures["VerifyPassword"]; err != nil {
		return false, err
	}
	return hash == "hash:"+password, nil
}

func (credentials *memoryCredentials) NewSessionToken() (string, string, error) {
	if err := credentials.failures["NewSessionToken"]; err != nil {
		return "", "", err
	}
	credentials.sequence++
	raw := fmt.Sprintf("raw-token-%d", credentials.sequence)
	return raw, credentials.HashSessionToken(raw), nil
}

func (*memoryCredentials) HashSessionToken(raw string) string { return "token:" + raw }

func (credentials *memoryCredentials) NewTemporaryPassword() (string, error) {
	if err := credentials.failures["NewTemporaryPassword"]; err != nil {
		return "", err
	}
	credentials.sequence++
	return fmt.Sprintf("temporary-password-%d", credentials.sequence), nil
}

type memoryAccessRepository struct {
	accounts           map[string]application.Account
	sessions           map[string]string
	analyticalSessions []application.AnalyticalSession
	nextAccount        int
	failures           map[string]error
	loginBlocked       bool
}

func (repository *memoryAccessRepository) CountAccounts(context.Context) (int64, error) {
	if err := repository.failures["CountAccounts"]; err != nil {
		return 0, err
	}
	return int64(len(repository.accounts)), nil
}

func (repository *memoryAccessRepository) CreateBootstrapAdmin(
	_ context.Context,
	account application.NewAccount,
) (bool, error) {
	if err := repository.failures["CreateBootstrapAdmin"]; err != nil {
		return false, err
	}
	if len(repository.accounts) > 0 {
		return false, nil
	}
	repository.storeAccount("admin-id", account)
	return true, nil
}

func (repository *memoryAccessRepository) FindAccountByLogin(
	_ context.Context,
	login string,
) (application.Account, bool, error) {
	if err := repository.failures["FindAccountByLogin"]; err != nil {
		return application.Account{}, false, err
	}
	for _, account := range repository.accounts {
		if strings.EqualFold(account.Login, login) {
			return account, true, nil
		}
	}
	return application.Account{}, false, nil
}

func (repository *memoryAccessRepository) StartSession(_ context.Context, session application.NewSession) error {
	if err := repository.failures["StartSession"]; err != nil {
		return err
	}
	repository.sessions[session.TokenHash] = session.AccountID
	return nil
}

func (repository *memoryAccessRepository) FindAccountBySession(
	_ context.Context,
	hash string,
	_ time.Time,
	_ time.Time,
) (application.Account, bool, error) {
	if err := repository.failures["FindAccountBySession"]; err != nil {
		return application.Account{}, false, err
	}
	accountID, found := repository.sessions[hash]
	account, exists := repository.accounts[accountID]
	return account, found && exists && account.State != application.AccountDeactivated, nil
}

func (repository *memoryAccessRepository) ActivateAccount(
	_ context.Context,
	accountID string,
	passwordHash string,
	session application.NewSession,
	_ time.Time,
) error {
	if err := repository.failures["ActivateAccount"]; err != nil {
		return err
	}
	account := repository.accounts[accountID]
	account.State = application.AccountActive
	account.PasswordHash = passwordHash
	account.TemporaryPasswordExpiresAt = nil
	repository.accounts[accountID] = account
	repository.revoke(accountID)
	return repository.StartSession(context.Background(), session)
}

func (repository *memoryAccessRepository) RevokeSession(
	_ context.Context,
	hash string,
	_ time.Time,
) error {
	if err := repository.failures["RevokeSession"]; err != nil {
		return err
	}
	delete(repository.sessions, hash)
	return nil
}

func (repository *memoryAccessRepository) LoginAllowed(
	context.Context,
	string,
	string,
	time.Time,
) (bool, time.Duration, error) {
	if err := repository.failures["LoginAllowed"]; err != nil {
		return false, 0, err
	}
	return !repository.loginBlocked, 0, nil
}

func (repository *memoryAccessRepository) RecordLoginFailure(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return repository.failures["RecordLoginFailure"]
}

func (repository *memoryAccessRepository) ClearLoginFailures(context.Context, string) error {
	return repository.failures["ClearLoginFailures"]
}

func (repository *memoryAccessRepository) ListAnalyticalSessions(
	_ context.Context,
	offset int,
	limit int,
) ([]application.AnalyticalSession, bool, error) {
	if err := repository.failures["ListAnalyticalSessions"]; err != nil {
		return nil, false, err
	}
	end := min(offset+limit, len(repository.analyticalSessions))
	return slices.Clone(repository.analyticalSessions[offset:end]), end < len(repository.analyticalSessions), nil
}

func (repository *memoryAccessRepository) GetAnalyticalSession(
	_ context.Context,
	id string,
) (application.AnalyticalSession, bool, error) {
	if err := repository.failures["GetAnalyticalSession"]; err != nil {
		return application.AnalyticalSession{}, false, err
	}
	for _, session := range repository.analyticalSessions {
		if session.ID == id {
			return session, true, nil
		}
	}
	return application.AnalyticalSession{}, false, nil
}

func (repository *memoryAccessRepository) ListAccounts(context.Context) ([]application.Account, error) {
	if err := repository.failures["ListAccounts"]; err != nil {
		return nil, err
	}
	accounts := make([]application.Account, 0, len(repository.accounts))
	for _, account := range repository.accounts {
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (repository *memoryAccessRepository) CreateAccount(
	_ context.Context,
	_ string,
	account application.NewAccount,
) (application.Account, error) {
	if err := repository.failures["CreateAccount"]; err != nil {
		return application.Account{}, err
	}
	repository.nextAccount++
	id := fmt.Sprintf("employee-%d", repository.nextAccount)
	return repository.storeAccount(id, account), nil
}

func (repository *memoryAccessRepository) UpdateAccount(
	_ context.Context,
	_ string,
	employeeID string,
	changes application.EmployeeChanges,
	_ time.Time,
) (application.Account, error) {
	if err := repository.failures["UpdateAccount"]; err != nil {
		return application.Account{}, err
	}
	account := repository.accounts[employeeID]
	if changes.DisplayName != nil {
		account.DisplayName = *changes.DisplayName
	}
	if changes.Role != nil {
		account.Role = *changes.Role
	}
	repository.accounts[employeeID] = account
	return account, nil
}

func (repository *memoryAccessRepository) DeactivateAccount(
	_ context.Context,
	_ string,
	employeeID string,
	_ time.Time,
) (application.Account, error) {
	if err := repository.failures["DeactivateAccount"]; err != nil {
		return application.Account{}, err
	}
	account := repository.accounts[employeeID]
	account.State = application.AccountDeactivated
	repository.accounts[employeeID] = account
	repository.revoke(employeeID)
	return account, nil
}

func (repository *memoryAccessRepository) IssueTemporaryPassword(
	_ context.Context,
	_ string,
	employeeID string,
	passwordHash string,
	expiresAt time.Time,
	_ time.Time,
	_ bool,
) (application.Account, error) {
	if err := repository.failures["IssueTemporaryPassword"]; err != nil {
		return application.Account{}, err
	}
	account := repository.accounts[employeeID]
	account.State = application.AccountPendingActivation
	account.PasswordHash = passwordHash
	account.TemporaryPasswordExpiresAt = &expiresAt
	repository.accounts[employeeID] = account
	repository.revoke(employeeID)
	return account, nil
}

func (repository *memoryAccessRepository) RevokeAllSessions(
	_ context.Context,
	_ string,
	employeeID string,
	_ time.Time,
) (bool, error) {
	if err := repository.failures["RevokeAllSessions"]; err != nil {
		return false, err
	}
	if _, found := repository.accounts[employeeID]; !found {
		return false, nil
	}
	repository.revoke(employeeID)
	return true, nil
}

func (repository *memoryAccessRepository) storeAccount(
	id string,
	newAccount application.NewAccount,
) application.Account {
	account := application.Account{
		ID: id, Login: newAccount.Login, DisplayName: newAccount.DisplayName,
		Role: newAccount.Role, State: newAccount.State, PasswordHash: newAccount.PasswordHash,
		TemporaryPasswordExpiresAt: &newAccount.TemporaryPasswordExpiresAt,
		CreatedAt:                  newAccount.CreatedAt, UpdatedAt: newAccount.CreatedAt,
	}
	repository.accounts[id] = account
	return account
}

func (repository *memoryAccessRepository) revoke(accountID string) {
	for hash, sessionAccountID := range repository.sessions {
		if sessionAccountID == accountID {
			delete(repository.sessions, hash)
		}
	}
}

func analyticalSession(id string, regions []string, complexes []string) application.AnalyticalSession {
	periodFrom := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodTo := time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC)
	return application.AnalyticalSession{
		ID: id, Regions: regions, ResidentialComplexes: complexes,
		Segments:   []string{"Первичная недвижимость"},
		PeriodFrom: &periodFrom,
		PeriodTo:   &periodTo,
	}
}
