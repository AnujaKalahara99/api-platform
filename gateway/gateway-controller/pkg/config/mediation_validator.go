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
	"fmt"

	api "github.com/wso2/api-platform/gateway/gateway-controller/pkg/api/generated"
)

// validateMediationData validates the spec section of a MediationApi configuration
func (v *APIValidator) validateMediationData(spec *api.MediationAPIData) []ValidationError {
	var errors []ValidationError

	// Validate displayName
	if spec.DisplayName == "" {
		errors = append(errors, ValidationError{
			Field:   "spec.displayName",
			Message: "API display name is required",
		})
	} else if len(spec.DisplayName) > 100 {
		errors = append(errors, ValidationError{
			Field:   "spec.displayName",
			Message: "API display name must be 1-100 characters",
		})
	} else if !v.urlFriendlyNameRegex.MatchString(spec.DisplayName) {
		errors = append(errors, ValidationError{
			Field:   "spec.displayName",
			Message: "API display name must be URL-friendly (only letters, numbers, spaces, hyphens, underscores, and dots allowed)",
		})
	}

	// Validate version
	if spec.Version == "" {
		errors = append(errors, ValidationError{
			Field:   "spec.version",
			Message: "API version is required",
		})
	} else if !v.versionRegex.MatchString(spec.Version) {
		errors = append(errors, ValidationError{
			Field:   "spec.version",
			Message: "API version must follow semantic versioning pattern (e.g., v1.0, v2.1.3)",
		})
	}

	// Validate context
	errors = append(errors, v.validateContext(spec.Context)...)

	// Validate entrypoints
	if len(spec.Entrypoints) == 0 {
		errors = append(errors, ValidationError{
			Field:   "spec.entrypoints",
			Message: "At least one entrypoint is required",
		})
	} else {
		namesSeen := make(map[string]bool)
		for i, ep := range spec.Entrypoints {
			if ep.Name == "" {
				errors = append(errors, ValidationError{
					Field:   fmt.Sprintf("spec.entrypoints[%d].name", i),
					Message: "Entrypoint name is required",
				})
			} else if namesSeen[ep.Name] {
				errors = append(errors, ValidationError{
					Field:   fmt.Sprintf("spec.entrypoints[%d].name", i),
					Message: fmt.Sprintf("Duplicate entrypoint name '%s'", ep.Name),
				})
			} else {
				namesSeen[ep.Name] = true
			}
		}
	}

	return errors
}
