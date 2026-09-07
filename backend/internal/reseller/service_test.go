package reseller

import (
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestBeginGatewayRequestLeavesNormalKeysOnLegacyPath(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Set("api_key", &service.APIKey{ID: 1, Key: "sk-normal"})
	if err := (&Service{}).BeginGatewayRequest(c); err != nil {
		t.Fatalf("normal key entered reseller path: %v", err)
	}
}

func TestBeginGatewayRequestRequiresFeatureAndUUIDBeforeDatabase(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Set("api_key", &service.APIKey{ID: 1, Key: apiKeyPrefix + "credential"})

	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "false")
	if err := (&Service{}).BeginGatewayRequest(c); err != ErrDisabled {
		t.Fatalf("disabled error = %v, want %v", err, ErrDisabled)
	}

	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	c.Request.Header.Set("X-Reseller-Request-ID", "not-a-uuid")
	if err := (&Service{}).BeginGatewayRequest(c); err == nil {
		t.Fatal("invalid request id was accepted")
	}
}

func TestBeginGatewayRequestRejectsDuplicateBeforeUpstream(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT rc.reseller_id").WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"reseller_id", "product_id", "group_id", "catalog_version", "cost_rate_multiplier", "allowed_cidrs", "user_id", "available_balance"}).
			AddRow(int64(2), int64(3), int64(4), int64(5), float64(0.35), []byte(`[]`), int64(12), 10.0),
	)
	mock.ExpectExec("INSERT INTO reseller_request_reservations").
		WithArgs(anyUUID{}, int64(2), int64(3), int64(7), int64(4), int64(5), float64(0.35)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request.Header.Set("X-Reseller-Request-ID", uuid.NewString())
	c.Set("api_key", &service.APIKey{ID: 7, Key: apiKeyPrefix + "credential", UserID: 12, User: &service.User{ID: 12}})
	if err := (&Service{db: db}).BeginGatewayRequest(c); err != ErrDuplicateRequest {
		t.Fatalf("duplicate error = %v, want %v", err, ErrDuplicateRequest)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDedicatedKeyFitsExistingDatabaseColumn(t *testing.T) {
	result, err := (&Service{randRead: func(p []byte) (int, error) {
		for i := range p {
			p[i] = 1
		}
		return len(p), nil
	}}).randomToken(apiKeyPrefix, 28)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) > 64 {
		t.Fatalf("key length = %d, exceeds api_keys.key VARCHAR(64)", len(result))
	}
}

func TestFailedGatewayRequestUsesReservedPricingSnapshot(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT rc.reseller_id").WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"reseller_id", "product_id", "group_id", "catalog_version", "cost_rate_multiplier", "allowed_cidrs", "user_id", "available_balance"}).
			AddRow(int64(2), int64(3), int64(4), int64(5), float64(0.35), []byte(`[]`), int64(12), 10.0),
	)
	mock.ExpectExec("INSERT INTO reseller_request_reservations").
		WithArgs(anyUUID{}, int64(2), int64(3), int64(7), int64(4), int64(5), float64(0.35)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO reseller_request_settlements").
		WithArgs(anyUUID{}, int64(2), int64(3), int64(4), float64(0.35), int64(5), "http_502").
		WillReturnResult(sqlmock.NewResult(1, 1))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("X-Reseller-Request-ID", uuid.NewString())
	c.Set("api_key", &service.APIKey{ID: 7, Key: apiKeyPrefix + "credential", UserID: 12, User: &service.User{ID: 12}})
	sut := &Service{db: db}
	if err := sut.BeginGatewayRequest(c); err != nil {
		t.Fatal(err)
	}
	c.Status(http.StatusBadGateway)
	sut.FinishGatewayRequest(c)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type anyUUID struct{}

func (anyUUID) Match(value driver.Value) bool {
	switch v := value.(type) {
	case string:
		_, err := uuid.Parse(v)
		return err == nil
	case []byte:
		_, err := uuid.Parse(string(v))
		return err == nil
	default:
		return false
	}
}
