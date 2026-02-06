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

package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/wso2/api-platform/gateway/gateway-controller/pkg/api/middleware"
	"github.com/wso2/api-platform/gateway/gateway-controller/pkg/models"
)

// Valid test certificate (generated with openssl)
const validTestCert = `-----BEGIN CERTIFICATE-----
MIIDkzCCAnugAwIBAgIUI92o4hdPPhGB4BFivBQnTe/RRjMwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMQswCQYDVQQHDAJTRjENMAsG
A1UECgwEVGVzdDELMAkGA1UECwwCSVQxFDASBgNVBAMMC2V4YW1wbGUuY29tMB4X
DTI2MDIwNjA5MzIwNloXDTI3MDIwNjA5MzIwNlowWTELMAkGA1UEBhMCVVMxCzAJ
BgNVBAgMAkNBMQswCQYDVQQHDAJTRjENMAsGA1UECgwEVGVzdDELMAkGA1UECwwC
SVQxFDASBgNVBAMMC2V4YW1wbGUuY29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAiMGvSiOweFnDEfeyspV9BK/d/QXXGPey91qjtP3QkToIEbQQngM1
L8omo4dVoyqivbr5ngAGg1dSmwYC2EudyDg7fvERydIhjhCxLG6aN8Zn41AxmNzj
X0cZjM/o/38PI5QSYaC18J5cvz4er9ZtEiRGa0Jm5O22O7BlcOGDxy1FCENmsLvs
iVpLYg193j8gzFc1QrfBG3Fkpil5VVLcdIDeFyuXFOO4/nRLLefOCIsMVebmi7hx
6tFaMrmZ2jZV7nbVHFEJ6JKPpPg+4fWiG5bP0YkG/jGeGdVUAIr56z37ZKw7v2OK
iu4vA2YbKl8nO0VP4zbnk21bUU/xYTbGzwIDAQABo1MwUTAdBgNVHQ4EFgQU1tRl
0lD0zDHgIT4vJblGH6Q9hTswHwYDVR0jBBgwFoAU1tRl0lD0zDHgIT4vJblGH6Q9
hTswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAUxndGWNtdNPk
we7+UrZN8oZhE3bWdN6YB+R66dz6jDqjQxg7H5Nj/xoXrYlJ1Zxm67jpFCsZxOZc
xRGZVCp8vJEIPbMcbAxqbJTBTOjNIXdIwJ0ZQVPdT56eJPTPNgvdcI2y2cZ+IkZl
7iZ+PkQeoy0pI/P8aYShLdsJLeDxuFDFbSN7Y/a5Sm6nfwjlU6TABy5SdgfSbqKD
NbLeQy2E3Qy/SIsy/361VLbUNWyK5LyJLdIrDd2n+gsmzQ/cgV7b/fsDw22BmELB
RMVr21DnDN4l9BDDs8384GT2VOkW+6+Xl6co6gwNYSVRhsdOlDe8NkFtpe4BFg9H
/lNmxfnpPg==
-----END CERTIFICATE-----`

// Certificate chain with two certificates
const certChain = validTestCert + `
-----BEGIN CERTIFICATE-----
MIIDkzCCAnugAwIBAgIUI92o4hdPPhGB4BFivBQnTe/RRjMwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMQswCQYDVQQHDAJTRjENMAsG
A1UECgwEVGVzdDELMAkGA1UECwwCSVQxFDASBgNVBAMMC2V4YW1wbGUuY29tMB4X
DTI2MDIwNjA5MzIwNloXDTI3MDIwNjA5MzIwNlowWTELMAkGA1UEBhMCVVMxCzAJ
BgNVBAgMAkNBMQswCQYDVQQHDAJTRjENMAsGA1UECgwEVGVzdDELMAkGA1UECwwC
SVQxFDASBgNVBAMMC2V4YW1wbGUuY29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAiMGvSiOweFnDEfeyspV9BK/d/QXXGPey91qjtP3QkToIEbQQngM1
L8omo4dVoyqivbr5ngAGg1dSmwYC2EudyDg7fvERydIhjhCxLG6aN8Zn41AxmNzj
X0cZjM/o/38PI5QSYaC18J5cvz4er9ZtEiRGa0Jm5O22O7BlcOGDxy1FCENmsLvs
iVpLYg193j8gzFc1QrfBG3Fkpil5VVLcdIDeFyuXFOO4/nRLLefOCIsMVebmi7hx
6tFaMrmZ2jZV7nbVHFEJ6JKPpPg+4fWiG5bP0YkG/jGeGdVUAIr56z37ZKw7v2OK
iu4vA2YbKl8nO0VP4zbnk21bUU/xYTbGzwIDAQABo1MwUTAdBgNVHQ4EFgQU1tRl
0lD0zDHgIT4vJblGH6Q9hTswHwYDVR0jBBgwFoAU1tRl0lD0zDHgIT4vJblGH6Q9
hTswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAUxndGWNtdNPk
we7+UrZN8oZhE3bWdN6YB+R66dz6jDqjQxg7H5Nj/xoXrYlJ1Zxm67jpFCsZxOZc
xRGZVCp8vJEIPbMcbAxqbJTBTOjNIXdIwJ0ZQVPdT56eJPTPNgvdcI2y2cZ+IkZl
7iZ+PkQeoy0pI/P8aYShLdsJLeDxuFDFbSN7Y/a5Sm6nfwjlU6TABy5SdgfSbqKD
NbLeQy2E3Qy/SIsy/361VLbUNWyK5LyJLdIrDd2n+gsmzQ/cgV7b/fsDw22BmELB
RMVr21DnDN4l9BDDs8384GT2VOkW+6+Xl6co6gwNYSVRhsdOlDe8NkFtpe4BFg9H
/lNmxfnpPg==
-----END CERTIFICATE-----`

// ============ Helper Function Tests ============
// These tests don't require mocking the snapshot manager

func TestExtractCertificateMetadata_Success(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	subject, issuer, notBefore, notAfter, err := server.extractCertificateMetadata([]byte(validTestCert))

	assert.NoError(t, err)
	assert.NotEmpty(t, subject)
	assert.NotEmpty(t, issuer)
	assert.False(t, notBefore.IsZero())
	assert.False(t, notAfter.IsZero())
	assert.Contains(t, subject, "example.com")
}

func TestExtractCertificateMetadata_MultipleCerts(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	// Should extract from first cert in chain
	subject, issuer, notBefore, notAfter, err := server.extractCertificateMetadata([]byte(certChain))

	assert.NoError(t, err)
	assert.NotEmpty(t, subject)
	assert.NotEmpty(t, issuer)
	assert.False(t, notBefore.IsZero())
	assert.False(t, notAfter.IsZero())
}

func TestExtractCertificateMetadata_InvalidPEM(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	_, _, _, _, err := server.extractCertificateMetadata([]byte("not a PEM"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid certificate found")
}

func TestExtractCertificateMetadata_NoCertificate(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	pemWithoutCert := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA...
-----END RSA PRIVATE KEY-----`

	_, _, _, _, err := server.extractCertificateMetadata([]byte(pemWithoutCert))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid certificate found")
}

func TestValidateCertificate_SingleCert(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	count, err := server.validateCertificate([]byte(validTestCert))

	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestValidateCertificate_CertChain(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	count, err := server.validateCertificate([]byte(certChain))

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestValidateCertificate_InvalidPEM(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	_, err := server.validateCertificate([]byte("not valid PEM"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid certificates found")
}

func TestValidateCertificate_NoCerts(t *testing.T) {
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	pemWithoutCert := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA...
-----END RSA PRIVATE KEY-----`

	_, err := server.validateCertificate([]byte(pemWithoutCert))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid certificates found")
}

// ============ ListCertificates Tests ============
// These tests don't need snapshot manager mocking

func TestListCertificates_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()

	// Pre-populate with certificates
	cert1 := &models.StoredCertificate{
		ID:          "cert-1",
		Name:        "test-cert-1",
		Certificate: []byte(validTestCert),
		Subject:     "CN=example.com",
		Issuer:      "CN=example.com",
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		CertCount:   1,
	}
	cert2 := &models.StoredCertificate{
		ID:          "cert-2",
		Name:        "test-cert-2",
		Certificate: []byte(validTestCert),
		Subject:     "CN=test.com",
		Issuer:      "CN=test.com",
		NotAfter:    time.Now().Add(180 * 24 * time.Hour),
		CertCount:   1,
	}
	mockDB.certs = []*models.StoredCertificate{cert1, cert2}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.GET("/certificates", server.ListCertificates)

	req := httptest.NewRequest(http.MethodGet, "/certificates", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListCertificatesResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, 2, resp.TotalCount)
	assert.Equal(t, len(validTestCert)*2, resp.TotalBytes)
	assert.Len(t, resp.Certificates, 2)
}

func TestListCertificates_EmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	mockDB.certs = []*models.StoredCertificate{}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.GET("/certificates", server.ListCertificates)

	req := httptest.NewRequest(http.MethodGet, "/certificates", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListCertificatesResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, 0, resp.TotalCount)
	assert.Equal(t, 0, resp.TotalBytes)
}

func TestListCertificates_DatabaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	mockDB.getErr = errors.New("database error")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.GET("/certificates", server.ListCertificates)

	req := httptest.NewRequest(http.MethodGet, "/certificates", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "Failed to list certificates")
}

func TestListCertificates_CalculatesTotalBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()

	cert1 := &models.StoredCertificate{
		ID:          "cert-1",
		Name:        "test-cert-1",
		Certificate: []byte("small cert"),
		NotAfter:    time.Now(),
		CertCount:   1,
	}
	cert2 := &models.StoredCertificate{
		ID:          "cert-2",
		Name:        "test-cert-2",
		Certificate: []byte("another small cert"),
		NotAfter:    time.Now(),
		CertCount:   1,
	}
	mockDB.certs = []*models.StoredCertificate{cert1, cert2}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.GET("/certificates", server.ListCertificates)

	req := httptest.NewRequest(http.MethodGet, "/certificates", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListCertificatesResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	expectedBytes := len("small cert") + len("another small cert")
	assert.Equal(t, expectedBytes, resp.TotalBytes)
}

// ============ Error Handling Tests ============
// These test various error conditions without needing full snapshot manager

func TestUploadCertificate_InvalidRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.POST("/certificates", server.UploadCertificate)

	req := httptest.NewRequest(http.MethodPost, "/certificates", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "Invalid request body")
}

func TestUploadCertificate_MissingRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.POST("/certificates", server.UploadCertificate)

	tests := []struct {
		name    string
		reqBody UploadCertificateRequest
	}{
		{
			name:    "Missing certificate",
			reqBody: UploadCertificateRequest{Name: "test"},
		},
		{
			name:    "Missing name",
			reqBody: UploadCertificateRequest{Certificate: validTestCert},
		},
		{
			name:    "Both missing",
			reqBody: UploadCertificateRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.reqBody)
			req := httptest.NewRequest(http.MethodPost, "/certificates", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestUploadCertificate_InvalidPEMFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.POST("/certificates", server.UploadCertificate)

	reqBody := UploadCertificateRequest{
		Name:        "test-cert",
		Certificate: "not a valid PEM certificate",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/certificates", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "Invalid certificate")
}

func TestUploadCertificate_DatabaseSaveError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	mockDB.saveErr = errors.New("database error")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.POST("/certificates", server.UploadCertificate)

	reqBody := UploadCertificateRequest{
		Name:        "test-cert",
		Certificate: validTestCert,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/certificates", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "Failed to save certificate")
}

func TestDeleteCertificate_EmptyID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB := NewMockStorage()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &APIServer{db: mockDB, logger: logger}

	router := gin.New()
	router.Use(middleware.CorrelationIDMiddleware(server.logger))
	router.DELETE("/certificates", func(c *gin.Context) {
		server.DeleteCertificate(c, "")
	})

	req := httptest.NewRequest(http.MethodDelete, "/certificates", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "error", resp["status"])
	assert.Contains(t, resp["message"], "Certificate ID is required")
}
