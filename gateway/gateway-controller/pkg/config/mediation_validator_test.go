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

package config

import (
	"strings"
	"testing"

	api "github.com/wso2/api-platform/gateway/gateway-controller/pkg/api/generated"
)

func createValidMediationAPIConfig() *api.APIConfiguration {
	config := &api.APIConfiguration{
		ApiVersion: api.APIConfigurationApiVersionGatewayApiPlatformWso2Comv1alpha1,
		Kind:       api.MediationApi,
		Metadata: api.Metadata{
			Name: "test-mediation",
		},
	}

	spec := api.MediationAPIData{
		DisplayName: "Test Mediation",
		Version:     "v1.0",
		Context:     "/mediation",
		Entrypoints: []api.MediationEntrypoint{
			{Name: "ws-in"},
		},
	}
	config.Spec.FromMediationAPIData(spec)

	return config
}

func TestAPIValidator_ValidateMediationApi_Valid(t *testing.T) {
	v := NewAPIValidator()
	config := createValidMediationAPIConfig()

	errors := v.Validate(config)
	if len(errors) != 0 {
		t.Errorf("expected no errors for valid MediationApi, got: %v", errors)
	}
}

func TestAPIValidator_ValidateMediationApi_Kind(t *testing.T) {
	v := NewAPIValidator()
	config := createValidMediationAPIConfig()

	// MediationApi kind should be accepted
	errors := v.Validate(config)
	for _, e := range errors {
		if e.Field == "kind" {
			t.Errorf("unexpected kind error: %v", e)
		}
	}
}

func TestAPIValidator_ValidateMediationApi_DisplayName(t *testing.T) {
	v := NewAPIValidator()

	tests := []struct {
		name        string
		displayName string
		wantError   bool
		errContains string
	}{
		{name: "Valid name", displayName: "My Mediation", wantError: false},
		{name: "Empty name", displayName: "", wantError: true, errContains: "required"},
		{name: "Name too long", displayName: strings.Repeat("a", 101), wantError: true, errContains: "1-100 characters"},
		{name: "Invalid characters", displayName: "test@#$%", wantError: true, errContains: "URL-friendly"},
		{name: "Valid with hyphens and dots", displayName: "test-api_v1.0", wantError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidMediationAPIConfig()
			spec, _ := config.Spec.AsMediationAPIData()
			spec.DisplayName = tt.displayName
			config.Spec.FromMediationAPIData(spec)

			errors := v.Validate(config)
			hasDisplayNameError := false
			for _, e := range errors {
				if e.Field == "spec.displayName" {
					hasDisplayNameError = true
					if tt.errContains != "" && !strings.Contains(e.Message, tt.errContains) {
						t.Errorf("expected error containing %q, got: %s", tt.errContains, e.Message)
					}
					break
				}
			}
			if tt.wantError && !hasDisplayNameError {
				t.Error("expected displayName error, got none")
			}
			if !tt.wantError && hasDisplayNameError {
				t.Error("unexpected displayName error")
			}
		})
	}
}

func TestAPIValidator_ValidateMediationApi_Version(t *testing.T) {
	v := NewAPIValidator()

	tests := []struct {
		name      string
		version   string
		wantError bool
	}{
		{name: "Valid v1.0", version: "v1.0", wantError: false},
		{name: "Valid v2.1.3", version: "v2.1.3", wantError: false},
		{name: "Valid 1.0", version: "1.0", wantError: false},
		{name: "Empty version", version: "", wantError: true},
		{name: "Invalid version", version: "invalid", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidMediationAPIConfig()
			spec, _ := config.Spec.AsMediationAPIData()
			spec.Version = tt.version
			config.Spec.FromMediationAPIData(spec)

			errors := v.Validate(config)
			hasVersionError := false
			for _, e := range errors {
				if e.Field == "spec.version" {
					hasVersionError = true
					break
				}
			}
			if tt.wantError && !hasVersionError {
				t.Error("expected version error, got none")
			}
			if !tt.wantError && hasVersionError {
				t.Error("unexpected version error")
			}
		})
	}
}

func TestAPIValidator_ValidateMediationApi_Context(t *testing.T) {
	v := NewAPIValidator()

	tests := []struct {
		name      string
		context   string
		wantError bool
	}{
		{name: "Valid context", context: "/mediation", wantError: false},
		{name: "Empty context", context: "", wantError: true},
		{name: "Without leading slash", context: "mediation", wantError: true},
		{name: "With trailing slash", context: "/mediation/", wantError: true},
		{name: "Root context", context: "/", wantError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidMediationAPIConfig()
			spec, _ := config.Spec.AsMediationAPIData()
			spec.Context = tt.context
			config.Spec.FromMediationAPIData(spec)

			errors := v.Validate(config)
			hasContextError := false
			for _, e := range errors {
				if e.Field == "spec.context" {
					hasContextError = true
					break
				}
			}
			if tt.wantError && !hasContextError {
				t.Errorf("expected context error, got none. Errors: %v", errors)
			}
			if !tt.wantError && hasContextError {
				t.Error("unexpected context error")
			}
		})
	}
}

func TestAPIValidator_ValidateMediationApi_Entrypoints(t *testing.T) {
	v := NewAPIValidator()

	tests := []struct {
		name        string
		entrypoints []api.MediationEntrypoint
		wantError   bool
		errField    string
	}{
		{
			name:        "Valid single entrypoint",
			entrypoints: []api.MediationEntrypoint{{Name: "ws-in"}},
			wantError:   false,
		},
		{
			name: "Valid multiple entrypoints",
			entrypoints: []api.MediationEntrypoint{
				{Name: "ws-in"},
				{Name: "sse-in"},
				{Name: "http-post"},
			},
			wantError: false,
		},
		{
			name:        "Empty entrypoints",
			entrypoints: []api.MediationEntrypoint{},
			wantError:   true,
			errField:    "spec.entrypoints",
		},
		{
			name:        "Entrypoint with empty name",
			entrypoints: []api.MediationEntrypoint{{Name: ""}},
			wantError:   true,
			errField:    "spec.entrypoints[0].name",
		},
		{
			name: "Duplicate entrypoint names",
			entrypoints: []api.MediationEntrypoint{
				{Name: "ws-in"},
				{Name: "ws-in"},
			},
			wantError: true,
			errField:  "spec.entrypoints[1].name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createValidMediationAPIConfig()
			spec, _ := config.Spec.AsMediationAPIData()
			spec.Entrypoints = tt.entrypoints
			config.Spec.FromMediationAPIData(spec)

			errors := v.Validate(config)
			hasExpectedError := false
			for _, e := range errors {
				if strings.HasPrefix(e.Field, tt.errField) {
					hasExpectedError = true
					break
				}
			}
			if tt.wantError && !hasExpectedError {
				t.Errorf("expected error for field %s, got: %v", tt.errField, errors)
			}
			if !tt.wantError && len(errors) > 0 {
				for _, e := range errors {
					if strings.Contains(e.Field, "entrypoints") {
						t.Errorf("unexpected entrypoints error: %v", e)
					}
				}
			}
		})
	}
}

func TestAPIValidator_ValidateMediationApi_MultipleErrors(t *testing.T) {
	v := NewAPIValidator()
	config := createValidMediationAPIConfig()
	spec, _ := config.Spec.AsMediationAPIData()
	spec.DisplayName = ""
	spec.Version = ""
	spec.Context = ""
	spec.Entrypoints = []api.MediationEntrypoint{}
	config.Spec.FromMediationAPIData(spec)

	errors := v.Validate(config)
	if len(errors) < 4 {
		t.Errorf("expected at least 4 errors (displayName, version, context, entrypoints), got %d: %v", len(errors), errors)
	}
}
