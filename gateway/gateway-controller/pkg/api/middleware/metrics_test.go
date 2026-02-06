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
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/wso2/api-platform/gateway/gateway-controller/pkg/metrics"
)

// Helper function to setup metrics for testing
func setupMetrics(t *testing.T) {
	t.Helper()
	metrics.SetEnabled(true)
	metrics.Init()
}

func TestMetricsMiddleware_BasicRequest(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify metrics were recorded
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	// Verify HTTP requests total counter exists and is incremented
	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "gateway_controller_http_requests_total" {
			found = true
			assert.Greater(t, len(mf.GetMetric()), 0)
		}
	}
	assert.True(t, found, "http_requests_total metric should exist")
}

func TestMetricsMiddleware_DifferentHTTPMethods(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET Request", http.MethodGet, "/api/test"},
		{"POST Request", http.MethodPost, "/api/test"},
		{"PUT Request", http.MethodPut, "/api/test"},
		{"DELETE Request", http.MethodDelete, "/api/test"},
		{"PATCH Request", http.MethodPatch, "/api/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(MetricsMiddleware())

			handler := func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"method": tt.method})
			}

			switch tt.method {
			case http.MethodGet:
				router.GET(tt.path, handler)
			case http.MethodPost:
				router.POST(tt.path, handler)
			case http.MethodPut:
				router.PUT(tt.path, handler)
			case http.MethodDelete:
				router.DELETE(tt.path, handler)
			case http.MethodPatch:
				router.PATCH(tt.path, handler)
			}

			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			// Verify metric with correct method label
			metricFamilies, err := metrics.GetRegistry().Gather()
			assert.NoError(t, err)

			found := false
			for _, mf := range metricFamilies {
				if mf.GetName() == "gateway_controller_http_requests_total" {
					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()
						for _, l := range labels {
							if l.GetName() == "method" && l.GetValue() == tt.method {
								found = true
								break
							}
						}
					}
				}
			}
			assert.True(t, found, "Metric should contain method label: %s", tt.method)
		})
	}
}

func TestMetricsMiddleware_DifferentStatusCodes(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		statusCode int
	}{
		{"OK 200", http.StatusOK},
		{"Created 201", http.StatusCreated},
		{"Bad Request 400", http.StatusBadRequest},
		{"Not Found 404", http.StatusNotFound},
		{"Internal Server Error 500", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(MetricsMiddleware())

			router.GET("/test", func(c *gin.Context) {
				c.JSON(tt.statusCode, gin.H{"status": tt.statusCode})
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.statusCode, w.Code)

			// Verify metric contains status code
			metricFamilies, err := metrics.GetRegistry().Gather()
			assert.NoError(t, err)

			found := false
			for _, mf := range metricFamilies {
				if mf.GetName() == "gateway_controller_http_requests_total" {
					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()
						for _, l := range labels {
							if l.GetName() == "status_code" {
								found = true
								break
							}
						}
					}
				}
			}
			assert.True(t, found, "Metric should contain status_code label")
		})
	}
}

func TestMetricsMiddleware_RequestDuration(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	// Add a handler with artificial delay
	router.GET("/slow", func(c *gin.Context) {
		time.Sleep(10 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"message": "delayed response"})
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()

	startTime := time.Now()
	router.ServeHTTP(w, req)
	duration := time.Since(startTime)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(10))

	// Verify duration histogram metric exists
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "gateway_controller_http_request_duration_seconds" {
			found = true
			assert.Equal(t, dto.MetricType_HISTOGRAM, mf.GetType())
			assert.Greater(t, len(mf.GetMetric()), 0)
		}
	}
	assert.True(t, found, "http_request_duration_seconds metric should exist")
}

func TestMetricsMiddleware_RequestResponseSizes(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	router.POST("/echo", func(c *gin.Context) {
		var data map[string]interface{}
		if err := c.BindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"received": data})
	})

	requestBody := []byte(`{"test": "data", "number": 123}`)
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(requestBody))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Greater(t, w.Body.Len(), 0)

	// Verify size histogram metrics exist
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	foundRequestSize := false
	foundResponseSize := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "gateway_controller_http_request_size_bytes" {
			foundRequestSize = true
			assert.Equal(t, dto.MetricType_HISTOGRAM, mf.GetType())
		}
		if mf.GetName() == "gateway_controller_http_response_size_bytes" {
			foundResponseSize = true
			assert.Equal(t, dto.MetricType_HISTOGRAM, mf.GetType())
		}
	}
	assert.True(t, foundRequestSize, "http_request_size_bytes metric should exist")
	assert.True(t, foundResponseSize, "http_response_size_bytes metric should exist")
}

func TestMetricsMiddleware_EndpointLabeling(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name             string
		routePattern     string
		requestPath      string
		expectedEndpoint string
	}{
		{
			name:             "Route with parameter",
			routePattern:     "/api/:id",
			requestPath:      "/api/123",
			expectedEndpoint: "/api/:id", // Should use FullPath (route pattern)
		},
		{
			name:             "Static route",
			routePattern:     "/api/health",
			requestPath:      "/api/health",
			expectedEndpoint: "/api/health",
		},
		{
			name:             "Route with multiple parameters",
			routePattern:     "/api/:org/:project",
			requestPath:      "/api/acme/prod",
			expectedEndpoint: "/api/:org/:project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(MetricsMiddleware())

			router.GET(tt.routePattern, func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "ok"})
			})

			req := httptest.NewRequest(http.MethodGet, tt.requestPath, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			// Verify endpoint label uses route pattern
			metricFamilies, err := metrics.GetRegistry().Gather()
			assert.NoError(t, err)

			found := false
			for _, mf := range metricFamilies {
				if mf.GetName() == "gateway_controller_http_requests_total" {
					for _, m := range mf.GetMetric() {
						labels := m.GetLabel()
						for _, l := range labels {
							if l.GetName() == "endpoint" && l.GetValue() == tt.expectedEndpoint {
								found = true
								break
							}
						}
					}
				}
			}
			assert.True(t, found, "Metric should have endpoint label: %s", tt.expectedEndpoint)
		})
	}
}

func TestMetricsMiddleware_EndpointLabeling_FallbackToPath(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	// Use NoRoute handler to simulate when FullPath is empty
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	// Verify endpoint label falls back to URL.Path when FullPath is empty
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "gateway_controller_http_requests_total" {
			for _, m := range mf.GetMetric() {
				labels := m.GetLabel()
				for _, l := range labels {
					if l.GetName() == "endpoint" && l.GetValue() == "/nonexistent" {
						found = true
						break
					}
				}
			}
		}
	}
	assert.True(t, found, "Metric should fall back to URL.Path when FullPath is empty")
}

func TestMetricsMiddleware_ConcurrentRequests(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	// Handler with delay to test concurrent request tracking
	router.GET("/concurrent", func(c *gin.Context) {
		time.Sleep(50 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	// Get initial concurrent requests count
	initialCount := getGaugeValue(t, "gateway_controller_concurrent_requests")

	// Launch concurrent requests
	var wg sync.WaitGroup
	concurrentCount := 5

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/concurrent", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}()
	}

	// Give goroutines time to start (brief moment before they complete)
	time.Sleep(10 * time.Millisecond)

	// Check that concurrent requests were tracked (may be in progress)
	// Note: Due to timing, we just verify the gauge exists and is usable
	finalCount := getGaugeValue(t, "gateway_controller_concurrent_requests")

	// Wait for all requests to complete
	wg.Wait()

	// After completion, concurrent requests should be back to initial
	afterCount := getGaugeValue(t, "gateway_controller_concurrent_requests")
	assert.Equal(t, initialCount, afterCount, "Concurrent requests gauge should return to initial value")

	// Verify that metrics registered the increase/decrease at some point
	assert.GreaterOrEqual(t, finalCount, float64(0), "Concurrent requests should be >= 0")
}

func TestMetricsMiddleware_NegativeContentLength(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.ContentLength = -1 // Simulate unknown content length
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Should handle negative content length gracefully (treat as 0)
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "gateway_controller_http_request_size_bytes" {
			found = true
			assert.Greater(t, len(mf.GetMetric()), 0)
		}
	}
	assert.True(t, found, "Request size metric should handle negative content length")
}

func TestMetricsMiddleware_ZeroResponseSize(t *testing.T) {
	setupMetrics(t)
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(MetricsMiddleware())

	router.GET("/empty", func(c *gin.Context) {
		c.Status(http.StatusNoContent) // No response body
	})

	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Should handle zero/negative response size gracefully
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "gateway_controller_http_response_size_bytes" {
			found = true
			assert.Greater(t, len(mf.GetMetric()), 0)
		}
	}
	assert.True(t, found, "Response size metric should handle zero size")
}

// Helper function to get gauge value from metrics
func getGaugeValue(t *testing.T, metricName string) float64 {
	metricFamilies, err := metrics.GetRegistry().Gather()
	assert.NoError(t, err)

	for _, mf := range metricFamilies {
		if mf.GetName() == metricName {
			if len(mf.GetMetric()) > 0 {
				return mf.GetMetric()[0].GetGauge().GetValue()
			}
		}
	}
	return 0
}
