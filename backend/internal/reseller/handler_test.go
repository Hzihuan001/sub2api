package reseller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestGatewayMiddlewareDoesNotLeakDatabaseErrors(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT rc.reseller_id").WithArgs(int64(7)).
		WillReturnError(errors.New("secret database details"))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(&Service{db: db})
	router.Use(func(c *gin.Context) {
		c.Set("api_key", &service.APIKey{ID: 7, Key: apiKeyPrefix + "credential"})
		c.Next()
	})
	router.Use(handler.GatewayMiddleware())
	router.POST("/v1/messages", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	request.Header.Set("X-Reseller-Request-ID", uuid.NewString())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "secret database details") {
		t.Fatalf("response leaked internal error: %s", recorder.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
