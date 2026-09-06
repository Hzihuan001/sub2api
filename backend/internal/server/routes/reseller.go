package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/reseller"
	"github.com/gin-gonic/gin"
)

func RegisterResellerRoutes(v1 *gin.RouterGroup, handler *reseller.Handler) {
	protocol := v1.Group("/reseller/v1")
	protocol.POST("/enrollments/exchange", handler.ExchangeEnrollment)
	protocol.POST("/tokens/refresh", handler.RefreshAccessToken)

	authenticated := protocol.Group("")
	authenticated.Use(handler.Auth())
	{
		authenticated.GET("/catalog", handler.Catalog)
		authenticated.POST("/credentials/rotate", handler.RotateCredential)
		authenticated.GET("/settlements", handler.ListSettlements)
		authenticated.GET("/settlements/:request_id", handler.GetSettlement)
		authenticated.GET("/balance", handler.Balance)
	}
}

func registerResellerAdminRoutes(admin *gin.RouterGroup, handler *reseller.Handler) {
	resellers := admin.Group("/resellers")
	{
		resellers.GET("", handler.ListTenants)
		resellers.POST("", handler.CreateTenant)
		resellers.PATCH("/:id", handler.UpdateTenant)
		resellers.GET("/:id/products", handler.ListProducts)
		resellers.POST("/:id/products", handler.UpsertProduct)
		resellers.POST("/:id/enrollments", handler.CreateEnrollment)
		resellers.POST("/:id/products/:product_id/credentials/rotate", handler.AdminRotateCredential)
		resellers.GET("/:id/settlements", handler.AdminListSettlements)
	}
}
