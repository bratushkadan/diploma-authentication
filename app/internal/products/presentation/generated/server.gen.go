// Package oapi_codegen provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.4.1 DO NOT EDIT.
package oapi_codegen

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/runtime"
)

// Error defines model for Error.
type Error struct {
	Errors []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// ListProductsRes defines model for ListProductsRes.
type ListProductsRes struct {
	NextPageToken string                   `json:"next_page_token"`
	Products      []ListProductsResProduct `json:"products"`
}

// ListProductsResProduct defines model for ListProductsResProduct.
type ListProductsResProduct struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	PictureUrl string `json:"picture_url"`
	SellerId   string `json:"seller_id"`
}

// ProductsListParams defines parameters for ProductsList.
type ProductsListParams struct {
	// Filter Filter, such as "seller.id=foo" or "seller.id=foo&name=bar&in_stock=*"
	Filter        string  `form:"filter" json:"filter"`
	NextPageToken *string `form:"nextPageToken,omitempty" json:"nextPageToken,omitempty"`
}

// ServerInterface represents all server handlers.
type ServerInterface interface {
	// List products
	// (GET /api/v1/products)
	ProductsList(c *gin.Context, params ProductsListParams)
}

// ServerInterfaceWrapper converts contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler            ServerInterface
	HandlerMiddlewares []MiddlewareFunc
	ErrorHandler       func(*gin.Context, error, int)
}

type MiddlewareFunc func(c *gin.Context)

// ProductsList operation middleware
func (siw *ServerInterfaceWrapper) ProductsList(c *gin.Context) {

	var err error

	// Parameter object where we will unmarshal all parameters from the context
	var params ProductsListParams

	// ------------- Required query parameter "filter" -------------

	if paramValue := c.Query("filter"); paramValue != "" {

	} else {
		siw.ErrorHandler(c, fmt.Errorf("Query argument filter is required, but not found"), http.StatusBadRequest)
		return
	}

	err = runtime.BindQueryParameter("form", true, true, "filter", c.Request.URL.Query(), &params.Filter)
	if err != nil {
		siw.ErrorHandler(c, fmt.Errorf("Invalid format for parameter filter: %w", err), http.StatusBadRequest)
		return
	}

	// ------------- Optional query parameter "nextPageToken" -------------

	err = runtime.BindQueryParameter("form", true, false, "nextPageToken", c.Request.URL.Query(), &params.NextPageToken)
	if err != nil {
		siw.ErrorHandler(c, fmt.Errorf("Invalid format for parameter nextPageToken: %w", err), http.StatusBadRequest)
		return
	}

	for _, middleware := range siw.HandlerMiddlewares {
		middleware(c)
		if c.IsAborted() {
			return
		}
	}

	siw.Handler.ProductsList(c, params)
}

// GinServerOptions provides options for the Gin server.
type GinServerOptions struct {
	BaseURL      string
	Middlewares  []MiddlewareFunc
	ErrorHandler func(*gin.Context, error, int)
}

// RegisterHandlers creates http.Handler with routing matching OpenAPI spec.
func RegisterHandlers(router gin.IRouter, si ServerInterface) {
	RegisterHandlersWithOptions(router, si, GinServerOptions{})
}

// RegisterHandlersWithOptions creates http.Handler with additional options
func RegisterHandlersWithOptions(router gin.IRouter, si ServerInterface, options GinServerOptions) {
	errorHandler := options.ErrorHandler
	if errorHandler == nil {
		errorHandler = func(c *gin.Context, err error, statusCode int) {
			c.JSON(statusCode, gin.H{"msg": err.Error()})
		}
	}

	wrapper := ServerInterfaceWrapper{
		Handler:            si,
		HandlerMiddlewares: options.Middlewares,
		ErrorHandler:       errorHandler,
	}

	router.GET(options.BaseURL+"/api/v1/products", wrapper.ProductsList)
}
