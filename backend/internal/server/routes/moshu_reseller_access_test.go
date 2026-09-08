package routes

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/moshureseller"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMoshuResellerManagementAccess(t *testing.T) {
	for _, role := range []string{"admin", "manager", "user"} {
		t.Run(role, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			r := gin.New()
			admin := r.Group("/admin")
			admin.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUserRole), role) })
			registerMoshuResellerRoutes(admin, &handler.Handlers{MoshuReseller: moshureseller.NewHandler(moshureseller.NewService(db, nil, nil))})
			if role != "user" {
				mock.ExpectQuery("SELECT base_url").WillReturnError(sql.ErrNoRows)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/admin/moshu-reseller/status", nil))
			if role == "user" {
				require.Equal(t, 403, w.Code)
			} else {
				require.Equal(t, 200, w.Code)
			}
			for _, path := range []string{"/enroll", "/products/1/credentials/rotate"} {
				w = httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/admin/moshu-reseller"+path, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				if role == "admin" && strings.Contains(path, "rotate") {
					continue
				}
				r.ServeHTTP(w, req)
				if role == "admin" {
					require.Equal(t, http.StatusBadRequest, w.Code)
				} else {
					require.Equal(t, http.StatusForbidden, w.Code)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
