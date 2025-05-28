// Package docs provides Swagger documentation for the API.
// @title RNG Service API
// @version 1.0
// @description API for accessing hardware-generated random data
// @termsOfService http://swagger.io/terms/
// Package docs provides Swagger documentation for the API.
package docs

// This file exists to satisfy import dependencies until the proper Swagger documentation is generated.
// It will be replaced by swag init command output.

// SwaggerInfo holds exported Swagger Info
var SwaggerInfo = struct {
	Version     string
	Host        string
	BasePath    string
	Schemes     []string
	Title       string
	Description string
}{
	Title:       "RNG Service API",
	Description: "API for accessing hardware-generated random data",
	Version:     "1.0",
	Host:        "localhost:8080",
	BasePath:    "/api/v1",
	Schemes:     []string{"http"},
}

// @contact.name API Support
// @contact.email support@rng-service.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /api/v1
