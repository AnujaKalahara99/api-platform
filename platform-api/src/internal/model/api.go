/*
 *  Copyright (c) 2025, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */

package model

import (
	"time"
)

// API represents an API entity in the platform
type API struct {
	ID              string           `json:"id" db:"uuid"`
	Handle          string           `json:"handle" db:"handle"`
	Name            string           `json:"name" db:"name"`
	Kind            string           `json:"kind" db:"kind"`
	Description     string           `json:"description,omitempty" db:"description"`
	Context         string           `json:"context" db:"context"`
	Version         string           `json:"version" db:"version"`
	CreatedBy       string           `json:"createdBy,omitempty" db:"created_by"`
	ProjectID       string           `json:"projectId" db:"project_uuid"`           // FK to Project.ID
	OrganizationID  string           `json:"organizationId" db:"organization_uuid"` // FK to Organization.ID
	CreatedAt       time.Time        `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt       time.Time        `json:"updatedAt,omitempty" db:"updated_at"`
	LifeCycleStatus string           `json:"lifeCycleStatus,omitempty" db:"lifecycle_status"`
	Transport       []string         `json:"transport,omitempty" db:"transport"`
	BackendServices []BackendService `json:"backend-services,omitempty"`
	Policies        []Policy         `json:"policies,omitempty"`
	Operations      []Operation      `json:"operations,omitempty"`
	Channels        []Channel        `json:"channels,omitempty"`
	Upstream        *UpstreamConfig  `json:"upstream,omitempty" db:"-"`
}

// TableName returns the table name for the API model
func (API) TableName() string {
	return "apis"
}

// APIMetadata contains minimal API information for handle-to-UUID resolution
type APIMetadata struct {
	ID             string `json:"id" db:"uuid"`
	Handle         string `json:"handle" db:"handle"`
	Name           string `json:"name" db:"name"`
	Version        string `json:"version" db:"version"`
	Kind           string `json:"kind" db:"kind"`
	OrganizationID string `json:"organizationId" db:"organization_uuid"`
}

// MTLSConfig represents mutual TLS configuration
type MTLSConfig struct {
	Enabled                    bool   `json:"enabled,omitempty"`
	EnforceIfClientCertPresent bool   `json:"enforceIfClientCertPresent,omitempty"`
	VerifyClient               bool   `json:"verifyClient,omitempty"`
	ClientCert                 string `json:"clientCert,omitempty"`
	ClientKey                  string `json:"clientKey,omitempty"`
	CACert                     string `json:"caCert,omitempty"`
}

// BackendService represents a backend service configuration
type BackendService struct {
	ID             string                `json:"id" db:"uuid"`
	OrganizationID string                `json:"organizationId" db:"organization_uuid"`
	Name           string                `json:"name" db:"name"`
	Description    string                `json:"description,omitempty" db:"description"`
	Endpoints      []BackendEndpoint     `json:"endpoints,omitempty"`
	Timeout        *TimeoutConfig        `json:"timeout,omitempty"`
	Retries        int                   `json:"retries,omitempty" db:"retries"`
	LoadBalance    *LoadBalanceConfig    `json:"loadBalance,omitempty"`
	CircuitBreaker *CircuitBreakerConfig `json:"circuitBreaker,omitempty"`
	CreatedAt      time.Time             `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt      time.Time             `json:"updatedAt,omitempty" db:"updated_at"`
}

// APIBackendService represents the association between an API and a backend service
type APIBackendService struct {
	ApiID            string `json:"apiId" db:"api_uuid"`
	BackendServiceID string `json:"backendServiceId" db:"backend_service_uuid"`
	IsDefault        bool   `json:"isDefault" db:"is_default"`
}

// BackendEndpoint represents a backend endpoint
type BackendEndpoint struct {
	URL         string             `json:"url,omitempty"`
	Description string             `json:"description,omitempty"`
	HealthCheck *HealthCheckConfig `json:"healthCheck,omitempty"`
	Weight      int                `json:"weight,omitempty"`
	MTLS        *MTLSConfig        `json:"mtls,omitempty"`
}

// HealthCheckConfig represents health check configuration
type HealthCheckConfig struct {
	Enabled            bool `json:"enabled,omitempty"`
	Interval           int  `json:"interval,omitempty"`
	Timeout            int  `json:"timeout,omitempty"`
	UnhealthyThreshold int  `json:"unhealthyThreshold,omitempty"`
	HealthyThreshold   int  `json:"healthyThreshold,omitempty"`
}

// TimeoutConfig represents timeout configuration
type TimeoutConfig struct {
	Connect int `json:"connect,omitempty"`
	Read    int `json:"read,omitempty"`
	Write   int `json:"write,omitempty"`
}

// LoadBalanceConfig represents load balancing configuration
type LoadBalanceConfig struct {
	Algorithm string `json:"algorithm,omitempty"`
	Failover  bool   `json:"failover,omitempty"`
}

// CircuitBreakerConfig represents circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled            bool `json:"enabled,omitempty"`
	MaxConnections     int  `json:"maxConnections,omitempty"`
	MaxPendingRequests int  `json:"maxPendingRequests,omitempty"`
	MaxRequests        int  `json:"maxRequests,omitempty"`
	MaxRetries         int  `json:"maxRetries,omitempty"`
}

// Operation represents an API operation
type Operation struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Request     *OperationRequest `json:"request,omitempty"`
}

// Channel represents an API channel
type Channel struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Request     *ChannelRequest `json:"request,omitempty"`
}

// OperationRequest represents operation request details
type OperationRequest struct {
	Method          string                `json:"method,omitempty"`
	Path            string                `json:"path,omitempty"`
	BackendServices []BackendRouting      `json:"backend-services,omitempty"`
	Authentication  *AuthenticationConfig `json:"authentication,omitempty"`
	Policies        []Policy              `json:"policies,omitempty"`
}

// ChannelRequest represents channel request details
type ChannelRequest struct {
	Method          string                `json:"method,omitempty"`
	Name            string                `json:"name,omitempty"`
	BackendServices []BackendRouting      `json:"backend-services,omitempty"`
	Authentication  *AuthenticationConfig `json:"authentication,omitempty"`
	Policies        []Policy              `json:"policies,omitempty"`
}

// BackendRouting represents backend routing configuration
type BackendRouting struct {
	Name   string `json:"name,omitempty"`
	Weight int    `json:"weight,omitempty"`
}

// AuthenticationConfig represents authentication configuration for operations
type AuthenticationConfig struct {
	Required bool     `json:"required,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
}

// Policy represents a request or response policy
type Policy struct {
	ExecutionCondition *string                 `json:"executionCondition,omitempty"`
	Name               string                  `json:"name"`
	Params             *map[string]interface{} `json:"params,omitempty"`
	Version            string                  `json:"version"`
}

// APIDeployment represents an immutable API deployment artifact
// Status and UpdatedAt are populated from deployment_status table via JOIN
// If Status is nil, the deployment is ARCHIVED (not currently active or undeployed)
type APIDeployment struct {
	DeploymentID     string                 `json:"deploymentId" db:"deployment_id"`
	Name             string                 `json:"name" db:"name"`
	ArtifactID       string                 `json:"artifactId" db:"artifact_uuid"`
	OrganizationID   string                 `json:"organizationId" db:"organization_uuid"`
	GatewayID        string                 `json:"gatewayId" db:"gateway_uuid"`
	BaseDeploymentID *string                `json:"baseDeploymentId,omitempty" db:"base_deployment_id"`
	Content          []byte                 `json:"-" db:"content"`
	Metadata         map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
	CreatedAt        time.Time              `json:"createdAt" db:"created_at"`

	// Lifecycle state fields (from deployment_status table via JOIN)
	// nil values indicate ARCHIVED state (no record in status table)
	Status    *DeploymentStatus `json:"status,omitempty" db:"status"`
	UpdatedAt *time.Time        `json:"updatedAt,omitempty" db:"status_updated_at"`
}

// TableName returns the table name for the APIDeployment model
func (APIDeployment) TableName() string {
	return "deployments"
}

// APIAssociation represents the association between an API and a resource (gateway or dev portal)
type APIAssociation struct {
	ID              int64     `json:"id" db:"id"`
	ArtifactID      string    `json:"artifactId" db:"artifact_uuid"`
	OrganizationID  string    `json:"organizationId" db:"organization_uuid"`
	ResourceID      string    `json:"resourceId" db:"resource_uuid"`
	AssociationType string    `json:"associationType" db:"association_type"`
	CreatedAt       time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt       time.Time `json:"updatedAt" db:"updated_at"`
}

// TableName returns the table name for the APIAssociation model
func (APIAssociation) TableName() string {
	return "association_mappings"
}

// DeploymentStatus represents the status of an API deployment
// Note: ARCHIVED is a derived state (not stored in database)
type DeploymentStatus string

const (
	DeploymentStatusDeployed   DeploymentStatus = "DEPLOYED"
	DeploymentStatusUndeployed DeploymentStatus = "UNDEPLOYED"
	DeploymentStatusArchived   DeploymentStatus = "ARCHIVED" // Derived state: exists in history but not in status table
)
