package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type managerGuardAdminService struct {
	service.AdminService
	users map[int64]*service.User
	keys  map[int64]*service.APIKey
}

func (s *managerGuardAdminService) GetUser(_ context.Context, id int64) (*service.User, error) {
	return s.users[id], nil
}

func (s *managerGuardAdminService) AdminUpdateAPIKeyGroupID(_ context.Context, id int64, _ *int64) (*service.AdminUpdateAPIKeyGroupIDResult, error) {
	return &service.AdminUpdateAPIKeyGroupIDResult{APIKey: s.keys[id]}, nil
}

func TestManagerTargetUserWriteGuardRejectsAdministrativeTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &UserHandler{adminService: &managerGuardAdminService{users: map[int64]*service.User{
		1: {ID: 1, Role: service.RoleAdmin},
		2: {ID: 2, Role: service.RoleManager},
		3: {ID: 3, Role: service.RoleUser},
	}}}

	for _, test := range []struct {
		id   string
		want int
	}{{"1", http.StatusForbidden}, {"2", http.StatusForbidden}, {"3", http.StatusNoContent}} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUserRole), service.RoleManager)
			c.Next()
		})
		router.PUT("/users/:id", handler.ManagerTargetUserWriteGuard(), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/users/"+test.id, nil))
		require.Equal(t, test.want, recorder.Code)
	}
}

func TestManagerBatchValidationIsAllOrNothing(t *testing.T) {
	handler := &UserHandler{adminService: &managerGuardAdminService{users: map[int64]*service.User{
		1: {ID: 1, Role: service.RoleUser},
		2: {ID: 2, Role: service.RoleManager},
	}}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/users/batch-limits", nil)
	c.Set(string(middleware.ContextKeyUserRole), service.RoleManager)

	require.False(t, handler.managerMayMutateUsers(c, []int64{1, 2}))
	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestManagerAPIKeyGuardRejectsAdministrativeOwners(t *testing.T) {
	adminService := &managerGuardAdminService{
		users: map[int64]*service.User{
			1: {ID: 1, Role: service.RoleUser},
			2: {ID: 2, Role: service.RoleAdmin},
		},
		keys: map[int64]*service.APIKey{
			10: {ID: 10, UserID: 1},
			20: {ID: 20, UserID: 2},
		},
	}
	handler := NewAdminAPIKeyHandler(adminService)

	for _, test := range []struct {
		keyID string
		want  int
	}{{"10", http.StatusNoContent}, {"20", http.StatusForbidden}} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUserRole), service.RoleManager)
			c.Next()
		})
		router.PUT("/api-keys/:id", handler.ManagerTargetUserWriteGuard(), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api-keys/"+test.keyID, nil))
		require.Equal(t, test.want, recorder.Code)
	}
}

func TestManagerCannotCreateAdministrativeUserWithForgedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &UserHandler{}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleManager)
		c.Next()
	})
	router.POST("/users", handler.Create)

	for _, role := range []string{service.RoleAdmin, service.RoleManager} {
		body := bytes.NewBufferString(`{"email":"new@example.com","password":"123456","role":"` + role + `"}`)
		request := httptest.NewRequest(http.MethodPost, "/users", body)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusForbidden, recorder.Code)
	}
}

func TestManagerCannotPromoteOrdinaryUserWithForgedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &UserHandler{adminService: &managerGuardAdminService{users: map[int64]*service.User{
		3: {ID: 3, Role: service.RoleUser},
	}}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleManager)
		c.Next()
	})
	router.PUT("/users/:id", handler.Update)

	for _, role := range []string{service.RoleAdmin, service.RoleManager} {
		body := bytes.NewBufferString(`{"role":"` + role + `"}`)
		request := httptest.NewRequest(http.MethodPut, "/users/3", body)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusForbidden, recorder.Code)
	}
}
