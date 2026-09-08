package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func registerMoshuResellerRoutes(admin *gin.RouterGroup, handlers *handler.Handlers) {
	reseller := admin.Group("/moshu-reseller")
	reseller.Use(middleware.SuperAdminOnly())
	{
		reseller.GET("/status", handlers.MoshuReseller.Status)
		reseller.POST("/enroll", handlers.MoshuReseller.Enroll)
		reseller.POST("/catalog/sync", handlers.MoshuReseller.SyncCatalog)
		reseller.PUT("/products/:id", handlers.MoshuReseller.ConfigureProduct)
		reseller.POST("/products/:id/test-account", handlers.MoshuReseller.EnsureProductTestAccount)
		reseller.POST("/products/:id/credentials/rotate", handlers.MoshuReseller.RotateCredential)
		reseller.POST("/settlements/sync", handlers.MoshuReseller.SyncSettlements)
		reseller.GET("/profits", handlers.MoshuReseller.ListProfits)
	}
}
