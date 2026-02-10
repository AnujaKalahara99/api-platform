/*
 *  Copyright (c) 2026, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
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

package service

import (
	"errors"
	"fmt"
	"log"

	"platform-api/src/config"
	"platform-api/src/internal/constants"
	"platform-api/src/internal/dto"
	"platform-api/src/internal/model"
	"platform-api/src/internal/repository"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// LLMProviderDeploymentService handles business logic for LLM provider deployment operations
// using the shared deployments table and status model.
type LLMProviderDeploymentService struct {
	providerRepo         repository.LLMProviderRepository
	templateRepo         repository.LLMProviderTemplateRepository
	deploymentRepo       repository.DeploymentRepository
	gatewayRepo          repository.GatewayRepository
	orgRepo              repository.OrganizationRepository
	gatewayEventsService *GatewayEventsService
	cfg                  *config.Server
}

// NewLLMProviderDeploymentService creates a new LLM provider deployment service
func NewLLMProviderDeploymentService(
	providerRepo repository.LLMProviderRepository,
	templateRepo repository.LLMProviderTemplateRepository,
	deploymentRepo repository.DeploymentRepository,
	gatewayRepo repository.GatewayRepository,
	orgRepo repository.OrganizationRepository,
	gatewayEventsService *GatewayEventsService,
	cfg *config.Server,
) *LLMProviderDeploymentService {
	return &LLMProviderDeploymentService{
		providerRepo:         providerRepo,
		templateRepo:         templateRepo,
		deploymentRepo:       deploymentRepo,
		gatewayRepo:          gatewayRepo,
		orgRepo:              orgRepo,
		gatewayEventsService: gatewayEventsService,
		cfg:                  cfg,
	}
}

// DeployLLMProvider creates a new immutable deployment artifact and deploys it to a gateway
func (s *LLMProviderDeploymentService) DeployLLMProvider(providerID string, req *dto.DeployAPIRequest, orgUUID string) (*dto.DeploymentResponse, error) {
	// Validate request
	if req.Base == "" {
		return nil, constants.ErrDeploymentBaseRequired
	}
	if req.GatewayID == "" {
		return nil, constants.ErrDeploymentGatewayIDRequired
	}

	// Validate gateway exists and belongs to organization
	gateway, err := s.gatewayRepo.GetByUUID(req.GatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway: %w", err)
	}
	if gateway == nil || gateway.OrganizationID != orgUUID {
		return nil, constants.ErrGatewayNotFound
	}

	// Get LLM provider
	provider, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	// Validate deployment name is provided
	if req.Name == "" {
		return nil, constants.ErrDeploymentNameRequired
	}

	var baseDeploymentID *string
	var contentBytes []byte

	// Determine the source: "current" or existing deployment
	if req.Base == "current" {
		tplHandle, err := s.getTemplateHandle(provider.TemplateUUID, orgUUID)
		if err != nil {
			return nil, err
		}
		providerYaml, err := generateLLMProviderDeploymentYAML(provider, tplHandle)
		if err != nil {
			return nil, fmt.Errorf("failed to generate LLM provider deployment YAML: %w", err)
		}
		contentBytes = []byte(providerYaml)
	} else {
		// Use existing deployment as base
		baseDeployment, err := s.deploymentRepo.GetWithContent(req.Base, provider.UUID, orgUUID)
		if err != nil {
			if errors.Is(err, constants.ErrDeploymentNotFound) {
				return nil, constants.ErrBaseDeploymentNotFound
			}
			return nil, fmt.Errorf("failed to get base deployment: %w", err)
		}
		contentBytes = baseDeployment.Content
		baseDeploymentID = &req.Base
	}

	// Generate deployment ID
	deploymentID := uuid.New().String()

	deployment := &model.Deployment{
		DeploymentID:     deploymentID,
		Name:             req.Name,
		ArtifactID:       provider.UUID,
		OrganizationID:   orgUUID,
		GatewayID:        req.GatewayID,
		BaseDeploymentID: baseDeploymentID,
		Content:          contentBytes,
		Metadata:         req.Metadata,
	}

	if s.cfg.Deployments.MaxPerAPIGateway < 1 {
		return nil, fmt.Errorf("MaxPerAPIGateway limit config must be at least 1, got %d", s.cfg.Deployments.MaxPerAPIGateway)
	}
	hardLimit := s.cfg.Deployments.MaxPerAPIGateway + constants.DeploymentLimitBuffer
	if err := s.deploymentRepo.CreateWithLimitEnforcement(deployment, hardLimit); err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}

	// Broadcast LLM provider deployment event to gateway
	if s.gatewayEventsService != nil {
		vhost := ""
		if provider.Configuration.VHost != nil {
			vhost = *provider.Configuration.VHost
		}
		deploymentEvent := &model.LLMProviderDeploymentEvent{
			ProviderId:   provider.ID,
			DeploymentID: deploymentID,
			Vhost:        vhost,
			Environment:  "production",
		}

		if err := s.gatewayEventsService.BroadcastLLMProviderDeploymentEvent(req.GatewayID, deploymentEvent); err != nil {
			log.Printf("[WARN] Failed to broadcast LLM provider deployment event: %v", err)
		}
	}

	deployedStatus := model.DeploymentStatusDeployed
	return &dto.DeploymentResponse{
		DeploymentID:     deployment.DeploymentID,
		Name:             deployment.Name,
		GatewayID:        deployment.GatewayID,
		Status:           string(deployedStatus),
		BaseDeploymentID: deployment.BaseDeploymentID,
		Metadata:         deployment.Metadata,
		CreatedAt:        deployment.CreatedAt,
		UpdatedAt:        deployment.UpdatedAt,
	}, nil
}

// RestoreLLMProviderDeployment restores a previous deployment (ARCHIVED or UNDEPLOYED)
func (s *LLMProviderDeploymentService) RestoreLLMProviderDeployment(providerID, deploymentID, gatewayID, orgUUID string) (*dto.DeploymentResponse, error) {
	provider, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	targetDeployment, err := s.deploymentRepo.GetWithContent(deploymentID, provider.UUID, orgUUID)
	if err != nil {
		return nil, err
	}
	if targetDeployment == nil {
		return nil, constants.ErrDeploymentNotFound
	}
	if targetDeployment.GatewayID != gatewayID {
		return nil, constants.ErrGatewayIDMismatch
	}

	currentDeploymentID, status, _, err := s.deploymentRepo.GetStatus(provider.UUID, orgUUID, targetDeployment.GatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment status: %w", err)
	}
	if currentDeploymentID == deploymentID && status == model.DeploymentStatusDeployed {
		return nil, constants.ErrDeploymentAlreadyDeployed
	}

	gateway, err := s.gatewayRepo.GetByUUID(targetDeployment.GatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway: %w", err)
	}
	if gateway == nil || gateway.OrganizationID != orgUUID {
		return nil, constants.ErrGatewayNotFound
	}

	updatedAt, err := s.deploymentRepo.SetCurrent(provider.UUID, orgUUID, targetDeployment.GatewayID, deploymentID, model.DeploymentStatusDeployed)
	if err != nil {
		return nil, fmt.Errorf("failed to set current deployment: %w", err)
	}

	// Broadcast LLM provider deployment event to gateway
	if s.gatewayEventsService != nil {
		vhost := ""
		if provider.Configuration.VHost != nil {
			vhost = *provider.Configuration.VHost
		}
		deploymentEvent := &model.LLMProviderDeploymentEvent{
			ProviderId:   provider.ID,
			DeploymentID: deploymentID,
			Vhost:        vhost,
			Environment:  "production",
		}

		if err := s.gatewayEventsService.BroadcastLLMProviderDeploymentEvent(targetDeployment.GatewayID, deploymentEvent); err != nil {
			log.Printf("[WARN] Failed to broadcast LLM provider deployment event: %v", err)
		}
	}

	deployedStatus := model.DeploymentStatusDeployed
	return &dto.DeploymentResponse{
		DeploymentID:     targetDeployment.DeploymentID,
		Name:             targetDeployment.Name,
		GatewayID:        targetDeployment.GatewayID,
		Status:           string(deployedStatus),
		BaseDeploymentID: targetDeployment.BaseDeploymentID,
		Metadata:         targetDeployment.Metadata,
		CreatedAt:        targetDeployment.CreatedAt,
		UpdatedAt:        &updatedAt,
	}, nil
}

// UndeployLLMProviderDeployment undeploys an active deployment
func (s *LLMProviderDeploymentService) UndeployLLMProviderDeployment(providerID, deploymentID, gatewayID, orgUUID string) (*dto.DeploymentResponse, error) {
	provider, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	deployment, err := s.deploymentRepo.GetWithState(deploymentID, provider.UUID, orgUUID)
	if err != nil {
		return nil, err
	}
	if deployment == nil {
		return nil, constants.ErrDeploymentNotFound
	}
	if deployment.GatewayID != gatewayID {
		return nil, constants.ErrGatewayIDMismatch
	}
	if deployment.Status == nil || *deployment.Status != model.DeploymentStatusDeployed {
		return nil, constants.ErrDeploymentNotActive
	}

	gateway, err := s.gatewayRepo.GetByUUID(deployment.GatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway: %w", err)
	}
	if gateway == nil {
		return nil, constants.ErrGatewayNotFound
	}

	newUpdatedAt, err := s.deploymentRepo.SetCurrent(provider.UUID, orgUUID, deployment.GatewayID, deploymentID, model.DeploymentStatusUndeployed)
	if err != nil {
		return nil, fmt.Errorf("failed to update deployment status: %w", err)
	}

	// Broadcast LLM provider undeployment event to gateway
	if s.gatewayEventsService != nil {
		vhost := ""
		if provider.Configuration.VHost != nil {
			vhost = *provider.Configuration.VHost
		}
		undeploymentEvent := &model.LLMProviderUndeploymentEvent{
			ProviderId:  provider.ID,
			Vhost:       vhost,
			Environment: "production",
		}

		if err := s.gatewayEventsService.BroadcastLLMProviderUndeploymentEvent(deployment.GatewayID, undeploymentEvent); err != nil {
			log.Printf("[WARN] Failed to broadcast LLM provider undeployment event: %v", err)
		}
	}

	undeployedStatus := model.DeploymentStatusUndeployed
	return &dto.DeploymentResponse{
		DeploymentID:     deployment.DeploymentID,
		Name:             deployment.Name,
		GatewayID:        deployment.GatewayID,
		Status:           string(undeployedStatus),
		BaseDeploymentID: deployment.BaseDeploymentID,
		Metadata:         deployment.Metadata,
		CreatedAt:        deployment.CreatedAt,
		UpdatedAt:        &newUpdatedAt,
	}, nil
}

// DeleteLLMProviderDeployment permanently deletes an undeployed deployment artifact
func (s *LLMProviderDeploymentService) DeleteLLMProviderDeployment(providerID, deploymentID, orgUUID string) error {
	provider, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return err
	}
	if provider == nil {
		return constants.ErrLLMProviderNotFound
	}

	deployment, err := s.deploymentRepo.GetWithState(deploymentID, provider.UUID, orgUUID)
	if err != nil {
		return err
	}
	if deployment == nil {
		return constants.ErrDeploymentNotFound
	}
	if deployment.Status != nil && *deployment.Status == model.DeploymentStatusDeployed {
		return constants.ErrDeploymentIsDeployed
	}

	if err := s.deploymentRepo.Delete(deploymentID, provider.UUID, orgUUID); err != nil {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	return nil
}

// GetLLMProviderDeployments retrieves all deployments for a provider with optional filters
func (s *LLMProviderDeploymentService) GetLLMProviderDeployments(providerID, orgUUID string, gatewayID *string, status *string) (*dto.DeploymentListResponse, error) {
	provider, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	if status != nil {
		validStatuses := map[string]bool{
			string(model.DeploymentStatusDeployed):   true,
			string(model.DeploymentStatusUndeployed): true,
			string(model.DeploymentStatusArchived):   true,
		}
		if !validStatuses[*status] {
			return nil, constants.ErrInvalidDeploymentStatus
		}
	}

	if s.cfg.Deployments.MaxPerAPIGateway < 1 {
		return nil, fmt.Errorf("MaxPerAPIGateway config value must be at least 1, got %d", s.cfg.Deployments.MaxPerAPIGateway)
	}
	deployments, err := s.deploymentRepo.GetDeploymentsWithState(provider.UUID, orgUUID, gatewayID, status, s.cfg.Deployments.MaxPerAPIGateway)
	if err != nil {
		return nil, err
	}

	deploymentDTOs := make([]*dto.DeploymentResponse, 0, len(deployments))
	for _, d := range deployments {
		deploymentDTOs = append(deploymentDTOs, &dto.DeploymentResponse{
			DeploymentID:     d.DeploymentID,
			Name:             d.Name,
			GatewayID:        d.GatewayID,
			Status:           string(*d.Status),
			BaseDeploymentID: d.BaseDeploymentID,
			Metadata:         d.Metadata,
			CreatedAt:        d.CreatedAt,
			UpdatedAt:        d.UpdatedAt,
		})
	}

	return &dto.DeploymentListResponse{
		Count: len(deploymentDTOs),
		List:  deploymentDTOs,
	}, nil
}

// GetLLMProviderDeployment retrieves a specific deployment by ID
func (s *LLMProviderDeploymentService) GetLLMProviderDeployment(providerID, deploymentID, orgUUID string) (*dto.DeploymentResponse, error) {
	provider, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	deployment, err := s.deploymentRepo.GetWithState(deploymentID, provider.UUID, orgUUID)
	if err != nil {
		return nil, err
	}
	if deployment == nil {
		return nil, constants.ErrDeploymentNotFound
	}

	return &dto.DeploymentResponse{
		DeploymentID:     deployment.DeploymentID,
		Name:             deployment.Name,
		GatewayID:        deployment.GatewayID,
		Status:           string(*deployment.Status),
		BaseDeploymentID: deployment.BaseDeploymentID,
		Metadata:         deployment.Metadata,
		CreatedAt:        deployment.CreatedAt,
		UpdatedAt:        deployment.UpdatedAt,
	}, nil
}

func (s *LLMProviderDeploymentService) getTemplateHandle(templateUUID, orgUUID string) (string, error) {
	if templateUUID == "" {
		return "", constants.ErrLLMProviderTemplateNotFound
	}
	tpl, err := s.templateRepo.GetByUUID(templateUUID, orgUUID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve template: %w", err)
	}
	if tpl == nil {
		return "", constants.ErrLLMProviderTemplateNotFound
	}
	return tpl.ID, nil
}

func generateLLMProviderDeploymentYAML(provider *model.LLMProvider, templateHandle string) (string, error) {
	if provider == nil {
		return "", errors.New("provider is required")
	}
	if templateHandle == "" {
		return "", errors.New("template handle is required")
	}
	if provider.Configuration.Upstream == nil || provider.Configuration.Upstream.Main == nil {
		return "", constants.ErrInvalidInput
	}
	main := provider.Configuration.Upstream.Main
	if main.URL == "" && main.Ref == "" {
		return "", constants.ErrInvalidInput
	}

	contextValue := "/"
	if provider.Configuration.Context != nil && *provider.Configuration.Context != "" {
		contextValue = *provider.Configuration.Context
	}
	vhostValue := ""
	if provider.Configuration.VHost != nil {
		vhostValue = *provider.Configuration.VHost
	}

	accessControl := dto.LLMAccessControl{Mode: "deny_all"}
	if provider.Configuration.AccessControl != nil {
		accessControl.Mode = provider.Configuration.AccessControl.Mode
		if len(provider.Configuration.AccessControl.Exceptions) > 0 {
			accessControl.Exceptions = make([]dto.RouteException, 0, len(provider.Configuration.AccessControl.Exceptions))
			for _, e := range provider.Configuration.AccessControl.Exceptions {
				accessControl.Exceptions = append(accessControl.Exceptions, dto.RouteException{Path: e.Path, Methods: e.Methods})
			}
		}
	}

	policies := make([]dto.LLMPolicy, 0, len(provider.Configuration.Policies))
	for _, p := range provider.Configuration.Policies {
		paths := make([]dto.LLMPolicyPath, 0, len(p.Paths))
		for _, pp := range p.Paths {
			paths = append(paths, dto.LLMPolicyPath{Path: pp.Path, Methods: pp.Methods, Params: pp.Params})
		}
		policies = append(policies, dto.LLMPolicy{Name: p.Name, Version: p.Version, Paths: paths})
	}

	upstream := dto.LLMUpstreamYAML{URL: main.URL, Ref: main.Ref}
	if main.Auth != nil {
		upstream.Auth = &dto.UpstreamAuth{
			Type:   main.Auth.Type,
			Header: main.Auth.Header,
			Value:  main.Auth.Value,
		}
	}

	providerDeployment := dto.LLMProviderDeploymentYAML{
		ApiVersion: "gateway.api-platform.wso2.com/v1alpha1",
		Kind:       constants.LLMProvider,
		Metadata: dto.DeploymentMetadata{
			Name: provider.ID,
		},
		Spec: dto.LLMProviderDeploymentSpec{
			DisplayName:   provider.Name,
			Version:       provider.Version,
			Context:       contextValue,
			VHost:         vhostValue,
			Template:      templateHandle,
			Upstream:      upstream,
			AccessControl: accessControl,
			Policies:      policies,
		},
	}

	yamlBytes, err := yaml.Marshal(providerDeployment)
	if err != nil {
		return "", fmt.Errorf("failed to marshal LLM provider to YAML: %w", err)
	}

	return string(yamlBytes), nil
}
