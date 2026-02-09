/*
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestLoggingMiddleware_BasicRequest tests logging middleware with a basic request
func TestLoggingMiddleware_BasicRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?param=value", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	// Verify log output
	logOutput := buf.String()
	assert.Contains(t, logOutput, "HTTP request")
	assert.Contains(t, logOutput, "method=GET")
	assert.Contains(t, logOutput, "path=/test")
	assert.Contains(t, logOutput, "status=200")
	assert.Contains(t, logOutput, "latency")
}

// TestLoggingMiddleware_WithCorrelationID tests logging with correlation ID
func TestLoggingMiddleware_WithCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(CorrelationIDMiddleware(logger))
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Correlation-ID", "test-corr-123")
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	// Verify correlation ID in logs
	logOutput := buf.String()
	assert.Contains(t, logOutput, "test-corr-123")
}

// TestLoggingMiddleware_DifferentStatusCodes tests logging with various status codes
func TestLoggingMiddleware_DifferentStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "200 OK", statusCode: http.StatusOK},
		{name: "201 Created", statusCode: http.StatusCreated},
		{name: "400 Bad Request", statusCode: http.StatusBadRequest},
		{name: "404 Not Found", statusCode: http.StatusNotFound},
		{name: "500 Internal Server Error", statusCode: http.StatusInternalServerError},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
			
			router := gin.New()
			router.Use(LoggingMiddleware(logger))
			router.GET("/test", func(c *gin.Context) {
				c.Status(tt.statusCode)
			})
			
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)
			
			assert.Equal(t, tt.statusCode, w.Code)
			
			logOutput := buf.String()
			// Just verify status is logged (exact format may vary)
			assert.Contains(t, logOutput, "status=")
		})
	}
}

// TestLoggingMiddleware_WithQuery tests logging request with query parameters
func TestLoggingMiddleware_WithQuery(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?key1=value1&key2=value2", nil)
	router.ServeHTTP(w, req)
	
	logOutput := buf.String()
	assert.Contains(t, logOutput, "query=")
	assert.Contains(t, logOutput, "key1=value1")
}

// TestLoggingMiddleware_UserAgent tests logging captures user agent
func TestLoggingMiddleware_UserAgent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "TestClient/1.0")
	router.ServeHTTP(w, req)
	
	logOutput := buf.String()
	assert.Contains(t, logOutput, "user_agent=TestClient/1.0")
}

// TestLoggingMiddleware_LatencyMeasurement tests that latency is measured
func TestLoggingMiddleware_LatencyMeasurement(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		// Simulate some processing time
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	
	logOutput := buf.String()
	// Latency should be present in logs
	assert.Contains(t, logOutput, "latency=")
}

// TestLoggingMiddleware_DifferentHTTPMethods tests logging different HTTP methods
func TestLoggingMiddleware_DifferentHTTPMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
			
			router := gin.New()
			router.Use(LoggingMiddleware(logger))
			router.Handle(method, "/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			
			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/test", nil)
			router.ServeHTTP(w, req)
			
			logOutput := buf.String()
			assert.Contains(t, logOutput, "method="+method)
		})
	}
}

// TestLoggingMiddleware_ClientIP tests that client IP is logged
func TestLoggingMiddleware_ClientIP(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	
	logOutput := buf.String()
	// Client IP should be in the logs
	assert.Contains(t, logOutput, "client_ip=")
}

// TestLoggingMiddleware_EmptyPath tests logging with root path
func TestLoggingMiddleware_EmptyPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	router.ServeHTTP(w, req)
	
	logOutput := buf.String()
	assert.Contains(t, logOutput, "path=/")
}

// TestLoggingMiddleware_FallbackToBaseLogger tests fallback when no correlation logger exists
func TestLoggingMiddleware_FallbackToBaseLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	
	// Use logging middleware WITHOUT correlation middleware
	router := gin.New()
	router.Use(LoggingMiddleware(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	})
	
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	// Should still log even without correlation middleware
	logOutput := buf.String()
	assert.True(t, len(logOutput) > 0)
	assert.Contains(t, logOutput, "HTTP request")
}
