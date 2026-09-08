package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func registerMoshuResellerRoutes(admin *gin.RouterGroup, handlers *handler.Handlers) {
	reseller := admin.Group("/moshu-reseller")
	reseller.Use(middleware.AdminOnly())
	{
		reseller.GET("/status", handlers.MoshuReseller.Status)
		reseller.GET("/balance", handlers.MoshuReseller.Balance)
		reseller.POST("/enroll", middleware.SuperAdminOnly(), handlers.MoshuReseller.Enroll)
		reseller.POST("/catalog/sync", handlers.MoshuReseller.SyncCatalog)
		reseller.PUT("/products/:id", handlers.MoshuReseller.ConfigureProduct)
		reseller.POST("/products/:id/test-account", handlers.MoshuReseller.EnsureProductTestAccount)
		reseller.POST("/products/:id/credentials/rotate", middleware.SuperAdminOnly(), handlers.MoshuReseller.RotateCredential)
		reseller.POST("/settlements/sync", middleware.SuperAdminOnly(), handlers.MoshuReseller.SyncSettlements)
		reseller.GET("/profits", middleware.SuperAdminOnly(), handlers.MoshuReseller.ListProfits)
	}
}
