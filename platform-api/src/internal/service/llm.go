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
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"platform-api/src/internal/constants"
	"platform-api/src/internal/dto"
	"platform-api/src/internal/model"
	"platform-api/src/internal/repository"
)

const (
	llmStatusPending  = "pending"
	llmStatusDeployed = "deployed"
	llmStatusFailed   = "failed"
)

type LLMProviderTemplateService struct {
	repo repository.LLMProviderTemplateRepository
}

type LLMProviderService struct {
	repo           repository.LLMProviderRepository
	templateRepo   repository.LLMProviderTemplateRepository
	orgRepo        repository.OrganizationRepository
	templateSeeder *LLMTemplateSeeder
}

type LLMProxyService struct {
	repo         repository.LLMProxyRepository
	providerRepo repository.LLMProviderRepository
	projectRepo  repository.ProjectRepository
}

func NewLLMProviderTemplateService(repo repository.LLMProviderTemplateRepository) *LLMProviderTemplateService {
	return &LLMProviderTemplateService{repo: repo}
}

func NewLLMProviderService(repo repository.LLMProviderRepository, templateRepo repository.LLMProviderTemplateRepository, orgRepo repository.OrganizationRepository, templateSeeder *LLMTemplateSeeder) *LLMProviderService {
	return &LLMProviderService{repo: repo, templateRepo: templateRepo, orgRepo: orgRepo, templateSeeder: templateSeeder}
}

func NewLLMProxyService(repo repository.LLMProxyRepository, providerRepo repository.LLMProviderRepository, projectRepo repository.ProjectRepository) *LLMProxyService {
	return &LLMProxyService{repo: repo, providerRepo: providerRepo, projectRepo: projectRepo}
}

func (s *LLMProviderTemplateService) Create(orgUUID, createdBy string, req *dto.LLMProviderTemplate) (*dto.LLMProviderTemplate, error) {
	if req == nil {
		return nil, constants.ErrInvalidInput
	}
	if req.ID == "" || req.Name == "" {
		return nil, constants.ErrInvalidInput
	}

	exists, err := s.repo.Exists(req.ID, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to check template exists: %w", err)
	}
	if exists {
		return nil, constants.ErrLLMProviderTemplateExists
	}

	m := &model.LLMProviderTemplate{
		OrganizationUUID: orgUUID,
		ID:               req.ID,
		Name:             req.Name,
		Description:      req.Description,
		CreatedBy:        createdBy,
		Metadata:         mapTemplateMetadata(req.Metadata),
		PromptTokens:     mapExtractionIdentifier(req.PromptTokens),
		CompletionTokens: mapExtractionIdentifier(req.CompletionTokens),
		TotalTokens:      mapExtractionIdentifier(req.TotalTokens),
		RemainingTokens:  mapExtractionIdentifier(req.RemainingTokens),
		RequestModel:     mapExtractionIdentifier(req.RequestModel),
		ResponseModel:    mapExtractionIdentifier(req.ResponseModel),
	}
	if err := s.repo.Create(m); err != nil {
		if isSQLiteUniqueConstraint(err) {
			return nil, constants.ErrLLMProviderTemplateExists
		}
		return nil, fmt.Errorf("failed to create template: %w", err)
	}

	return mapTemplateModelToDTO(m), nil
}

func (s *LLMProviderTemplateService) List(orgUUID string, limit, offset int) (*dto.LLMProviderTemplateListResponse, error) {
	items, err := s.repo.List(orgUUID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	totalCount, err := s.repo.Count(orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to count templates: %w", err)
	}
	resp := &dto.LLMProviderTemplateListResponse{
		Count: len(items),
		Pagination: dto.Pagination{
			Limit:  limit,
			Offset: offset,
			Total:  totalCount,
		},
	}
	resp.List = make([]dto.LLMProviderTemplateListItem, 0, len(items))
	for _, t := range items {
		resp.List = append(resp.List, dto.LLMProviderTemplateListItem{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			CreatedBy:   t.CreatedBy,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *LLMProviderTemplateService) Get(orgUUID, handle string) (*dto.LLMProviderTemplate, error) {
	if handle == "" {
		return nil, constants.ErrInvalidInput
	}
	m, err := s.repo.GetByID(handle, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get template: %w", err)
	}
	if m == nil {
		return nil, constants.ErrLLMProviderTemplateNotFound
	}
	return mapTemplateModelToDTO(m), nil
}

func (s *LLMProviderTemplateService) Update(orgUUID, handle string, req *dto.LLMProviderTemplate) (*dto.LLMProviderTemplate, error) {
	if handle == "" || req == nil {
		return nil, constants.ErrInvalidInput
	}
	if req.ID != "" && req.ID != handle {
		return nil, constants.ErrInvalidInput
	}
	if req.Name == "" {
		return nil, constants.ErrInvalidInput
	}

	m := &model.LLMProviderTemplate{
		OrganizationUUID: orgUUID,
		ID:               handle,
		Name:             req.Name,
		Description:      req.Description,
		Metadata:         mapTemplateMetadata(req.Metadata),
		PromptTokens:     mapExtractionIdentifier(req.PromptTokens),
		CompletionTokens: mapExtractionIdentifier(req.CompletionTokens),
		TotalTokens:      mapExtractionIdentifier(req.TotalTokens),
		RemainingTokens:  mapExtractionIdentifier(req.RemainingTokens),
		RequestModel:     mapExtractionIdentifier(req.RequestModel),
		ResponseModel:    mapExtractionIdentifier(req.ResponseModel),
	}

	if err := s.repo.Update(m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, constants.ErrLLMProviderTemplateNotFound
		}
		return nil, fmt.Errorf("failed to update template: %w", err)
	}

	updated, err := s.repo.GetByID(handle, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated template: %w", err)
	}
	if updated == nil {
		return nil, constants.ErrLLMProviderTemplateNotFound
	}
	return mapTemplateModelToDTO(updated), nil
}

func (s *LLMProviderTemplateService) Delete(orgUUID, handle string) error {
	if handle == "" {
		return constants.ErrInvalidInput
	}
	if err := s.repo.Delete(handle, orgUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return constants.ErrLLMProviderTemplateNotFound
		}
		return fmt.Errorf("failed to delete template: %w", err)
	}
	return nil
}

func (s *LLMProviderService) Create(orgUUID, createdBy string, req *dto.LLMProvider) (*dto.LLMProvider, error) {
	if req == nil {
		return nil, constants.ErrInvalidInput
	}
	if req.ID == "" || req.Name == "" || req.Version == "" || req.Template == "" {
		return nil, constants.ErrInvalidInput
	}
	if err := validateModelProviders(req.Template, req.ModelProviders); err != nil {
		return nil, err
	}
	if s.orgRepo != nil {
		org, err := s.orgRepo.GetOrganizationByUUID(orgUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to validate organization: %w", err)
		}
		if org == nil {
			return nil, constants.ErrOrganizationNotFound
		}
	}

	if err := validateUpstream(req.Upstream); err != nil {
		return nil, err
	}
	if err := validateRateLimitingConfig(req.RateLimiting); err != nil {
		return nil, err
	}

	// Ensure template exists
	tpl, err := s.templateRepo.GetByID(req.Template, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate template: %w", err)
	}
	if tpl == nil && s.templateSeeder != nil {
		// Try to seed defaults for this org and re-fetch
		if seedErr := s.templateSeeder.SeedForOrg(orgUUID); seedErr != nil {
			return nil, fmt.Errorf("failed to seed default templates: %w", seedErr)
		}
		tpl, err = s.templateRepo.GetByID(req.Template, orgUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to validate template after seeding: %w", err)
		}
	}
	if tpl == nil {
		return nil, constants.ErrLLMProviderTemplateNotFound
	}

	exists, err := s.repo.Exists(req.ID, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to check provider exists: %w", err)
	}
	if exists {
		return nil, constants.ErrLLMProviderExists
	}

	contextValue := defaultString(req.Context, "/")
	m := &model.LLMProvider{
		OrganizationUUID: orgUUID,
		ID:               req.ID,
		Name:             req.Name,
		Description:      req.Description,
		CreatedBy:        createdBy,
		Version:          req.Version,
		TemplateUUID:     tpl.UUID,
		OpenAPISpec:      req.OpenAPI,
		ModelProviders:   mapModelProviders(req.ModelProviders),
		Status:           llmStatusPending,
		Configuration: model.LLMProviderConfig{
			Context:       &contextValue,
			VHost:         &req.VHost,
			Upstream:      mapUpstreamConfig(req.Upstream),
			AccessControl: mapAccessControl(&req.AccessControl),
			RateLimiting:  mapRateLimiting(req.RateLimiting),
			Policies:      mapPolicies(req.Policies),
			Security:      mapSecurityDTOToModel(req.Security),
		},
	}

	if err := s.repo.Create(m); err != nil {
		if isSQLiteUniqueConstraint(err) {
			return nil, constants.ErrLLMProviderExists
		}
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	created, err := s.repo.GetByID(req.ID, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created provider: %w", err)
	}
	if created == nil {
		return nil, constants.ErrLLMProviderNotFound
	}
	return mapProviderModelToDTO(created, tpl.ID), nil
}

func (s *LLMProviderService) List(orgUUID string, limit, offset int) (*dto.LLMProviderListResponse, error) {
	items, err := s.repo.List(orgUUID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	totalCount, err := s.repo.Count(orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to count providers: %w", err)
	}
	resp := &dto.LLMProviderListResponse{
		Count: len(items),
		Pagination: dto.Pagination{
			Limit:  limit,
			Offset: offset,
			Total:  totalCount,
		},
	}
	resp.List = make([]dto.LLMProviderListItem, 0, len(items))
	for _, p := range items {
		// Look up template handle from UUID
		tplHandle := ""
		if p.TemplateUUID != "" {
			tpl, err := s.templateRepo.GetByUUID(p.TemplateUUID, orgUUID)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve template for provider %s: %w", p.ID, err)
			}
			if tpl != nil {
				tplHandle = tpl.ID
			}
		}
		resp.List = append(resp.List, dto.LLMProviderListItem{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			CreatedBy:   p.CreatedBy,
			Version:     p.Version,
			Template:    tplHandle,
			Status:      p.Status,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *LLMProviderService) Get(orgUUID, handle string) (*dto.LLMProvider, error) {
	if handle == "" {
		return nil, constants.ErrInvalidInput
	}
	m, err := s.repo.GetByID(handle, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}
	if m == nil {
		return nil, constants.ErrLLMProviderNotFound
	}
	// Look up template handle from UUID
	tplHandle := ""
	if m.TemplateUUID != "" {
		tpl, err := s.templateRepo.GetByUUID(m.TemplateUUID, orgUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve template for provider %s: %w", m.ID, err)
		}
		if tpl != nil {
			tplHandle = tpl.ID
		}
	}
	return mapProviderModelToDTO(m, tplHandle), nil
}

func (s *LLMProviderService) Update(orgUUID, handle string, req *dto.LLMProvider) (*dto.LLMProvider, error) {
	if handle == "" || req == nil {
		return nil, constants.ErrInvalidInput
	}
	if req.ID != "" && req.ID != handle {
		return nil, constants.ErrInvalidInput
	}
	if req.Name == "" || req.Version == "" || req.Template == "" {
		return nil, constants.ErrInvalidInput
	}
	if err := validateModelProviders(req.Template, req.ModelProviders); err != nil {
		return nil, err
	}
	if err := validateUpstream(req.Upstream); err != nil {
		return nil, err
	}
	if err := validateRateLimitingConfig(req.RateLimiting); err != nil {
		return nil, err
	}

	// Validate template exists
	tpl, err := s.templateRepo.GetByID(req.Template, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate template: %w", err)
	}
	if tpl == nil {
		return nil, constants.ErrLLMProviderTemplateNotFound
	}

	contextValue := defaultString(req.Context, "/")
	m := &model.LLMProvider{
		OrganizationUUID: orgUUID,
		ID:               handle,
		Name:             req.Name,
		Description:      req.Description,
		Version:          req.Version,
		TemplateUUID:     tpl.UUID,
		OpenAPISpec:      req.OpenAPI,
		ModelProviders:   mapModelProviders(req.ModelProviders),
		Status:           llmStatusPending,
		Configuration: model.LLMProviderConfig{
			Context:       &contextValue,
			VHost:         &req.VHost,
			Upstream:      mapUpstreamConfig(req.Upstream),
			AccessControl: mapAccessControl(&req.AccessControl),
			RateLimiting:  mapRateLimiting(req.RateLimiting),
			Policies:      mapPolicies(req.Policies),
			Security:      mapSecurityDTOToModel(req.Security),
		},
	}

	if err := s.repo.Update(m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, constants.ErrLLMProviderNotFound
		}
		return nil, fmt.Errorf("failed to update provider: %w", err)
	}

	updated, err := s.repo.GetByID(handle, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated provider: %w", err)
	}
	if updated == nil {
		return nil, constants.ErrLLMProviderNotFound
	}
	return mapProviderModelToDTO(updated, tpl.ID), nil
}

func (s *LLMProviderService) Delete(orgUUID, handle string) error {
	if handle == "" {
		return constants.ErrInvalidInput
	}
	if err := s.repo.Delete(handle, orgUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return constants.ErrLLMProviderNotFound
		}
		return fmt.Errorf("failed to delete provider: %w", err)
	}
	return nil
}

func (s *LLMProxyService) Create(orgUUID, createdBy string, req *dto.LLMProxy) (*dto.LLMProxy, error) {
	if req == nil {
		return nil, constants.ErrInvalidInput
	}
	if req.ID == "" || req.Name == "" || req.Version == "" || req.Provider == "" || req.ProjectID == "" {
		return nil, constants.ErrInvalidInput
	}
	if s.projectRepo != nil {
		project, err := s.projectRepo.GetProjectByUUID(req.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to validate project: %w", err)
		}
		if project == nil || project.OrganizationID != orgUUID {
			return nil, constants.ErrProjectNotFound
		}
	}

	// Validate provider exists
	prov, err := s.providerRepo.GetByID(req.Provider, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate provider: %w", err)
	}
	if prov == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	exists, err := s.repo.Exists(req.ID, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to check proxy exists: %w", err)
	}
	if exists {
		return nil, constants.ErrLLMProxyExists
	}

	contextValue := defaultString(req.Context, "/")
	m := &model.LLMProxy{
		OrganizationUUID: orgUUID,
		ProjectUUID:      req.ProjectID,
		ID:               req.ID,
		Name:             req.Name,
		Description:      req.Description,
		CreatedBy:        createdBy,
		Version:          req.Version,
		ProviderUUID:     prov.UUID,
		OpenAPISpec:      req.OpenAPI,
		Status:           llmStatusPending,
		Configuration: model.LLMProxyConfig{
			Context:  &contextValue,
			Vhost:    &req.VHost,
			Provider: req.Provider,
			Policies: mapPolicies(req.Policies),
			Security: mapSecurityDTOToModel(req.Security),
		},
	}

	if err := s.repo.Create(m); err != nil {
		if isSQLiteUniqueConstraint(err) {
			return nil, constants.ErrLLMProxyExists
		}
		return nil, fmt.Errorf("failed to create proxy: %w", err)
	}

	created, err := s.repo.GetByID(req.ID, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created proxy: %w", err)
	}
	if created == nil {
		return nil, constants.ErrLLMProxyNotFound
	}
	return mapProxyModelToDTO(created), nil
}

func (s *LLMProxyService) List(orgUUID string, projectUUID *string, limit, offset int) (*dto.LLMProxyListResponse, error) {
	if projectUUID != nil && *projectUUID != "" && s.projectRepo != nil {
		project, err := s.projectRepo.GetProjectByUUID(*projectUUID)
		if err != nil {
			return nil, fmt.Errorf("failed to validate project: %w", err)
		}
		if project == nil || project.OrganizationID != orgUUID {
			return nil, constants.ErrProjectNotFound
		}
	}

	var items []*model.LLMProxy
	var err error
	if projectUUID != nil && *projectUUID != "" {
		items, err = s.repo.ListByProject(orgUUID, *projectUUID, limit, offset)
	} else {
		items, err = s.repo.List(orgUUID, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list proxies: %w", err)
	}
	var totalCount int
	if projectUUID != nil && *projectUUID != "" {
		totalCount, err = s.repo.CountByProject(orgUUID, *projectUUID)
	} else {
		totalCount, err = s.repo.Count(orgUUID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to count proxies: %w", err)
	}
	resp := &dto.LLMProxyListResponse{
		Count: len(items),
		Pagination: dto.Pagination{
			Limit:  limit,
			Offset: offset,
			Total:  totalCount,
		},
	}
	resp.List = make([]dto.LLMProxyListItem, 0, len(items))
	for _, p := range items {
		resp.List = append(resp.List, dto.LLMProxyListItem{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			CreatedBy:   p.CreatedBy,
			Version:     p.Version,
			ProjectID:   p.ProjectUUID,
			Provider:    p.Configuration.Provider,
			Status:      p.Status,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *LLMProxyService) ListByProvider(orgUUID, providerID string, limit, offset int) (*dto.LLMProxyListResponse, error) {
	if providerID == "" {
		return nil, constants.ErrInvalidInput
	}
	if s.providerRepo == nil {
		return nil, fmt.Errorf("could not initialize llmprovider repository")
	}
	prov, err := s.providerRepo.GetByID(providerID, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate provider: %w", err)
	}
	if prov == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	items, err := s.repo.ListByProvider(orgUUID, prov.UUID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list proxies by provider: %w", err)
	}
	totalCount, err := s.repo.CountByProvider(orgUUID, prov.UUID)
	if err != nil {
		return nil, fmt.Errorf("failed to count proxies by provider: %w", err)
	}
	resp := &dto.LLMProxyListResponse{
		Count: len(items),
		Pagination: dto.Pagination{
			Limit:  limit,
			Offset: offset,
			Total:  totalCount,
		},
	}
	resp.List = make([]dto.LLMProxyListItem, 0, len(items))
	for _, p := range items {
		resp.List = append(resp.List, dto.LLMProxyListItem{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			CreatedBy:   p.CreatedBy,
			Version:     p.Version,
			ProjectID:   p.ProjectUUID,
			Provider:    p.Configuration.Provider,
			Status:      p.Status,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}
	return resp, nil
}

func (s *LLMProxyService) Get(orgUUID, handle string) (*dto.LLMProxy, error) {
	if handle == "" {
		return nil, constants.ErrInvalidInput
	}
	m, err := s.repo.GetByID(handle, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy: %w", err)
	}
	if m == nil {
		return nil, constants.ErrLLMProxyNotFound
	}
	return mapProxyModelToDTO(m), nil
}

func (s *LLMProxyService) Update(orgUUID, handle string, req *dto.LLMProxy) (*dto.LLMProxy, error) {
	if handle == "" || req == nil {
		return nil, constants.ErrInvalidInput
	}
	if req.ID != "" && req.ID != handle {
		return nil, constants.ErrInvalidInput
	}
	if req.Name == "" || req.Version == "" || req.Provider == "" {
		return nil, constants.ErrInvalidInput
	}

	// Validate provider exists
	prov, err := s.providerRepo.GetByID(req.Provider, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate provider: %w", err)
	}
	if prov == nil {
		return nil, constants.ErrLLMProviderNotFound
	}

	contextValue := defaultString(req.Context, "/")
	m := &model.LLMProxy{
		OrganizationUUID: orgUUID,
		ID:               handle,
		Name:             req.Name,
		Description:      req.Description,
		Version:          req.Version,
		ProviderUUID:     prov.UUID,
		OpenAPISpec:      req.OpenAPI,
		Status:           llmStatusPending,
		Configuration: model.LLMProxyConfig{
			Context:  &contextValue,
			Vhost:    &req.VHost,
			Provider: req.Provider,
			Policies: mapPolicies(req.Policies),
			Security: mapSecurityDTOToModel(req.Security),
		},
	}
	if err := s.repo.Update(m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, constants.ErrLLMProxyNotFound
		}
		return nil, fmt.Errorf("failed to update proxy: %w", err)
	}

	updated, err := s.repo.GetByID(handle, orgUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated proxy: %w", err)
	}
	if updated == nil {
		return nil, constants.ErrLLMProxyNotFound
	}
	return mapProxyModelToDTO(updated), nil
}

func (s *LLMProxyService) Delete(orgUUID, handle string) error {
	if handle == "" {
		return constants.ErrInvalidInput
	}
	if err := s.repo.Delete(handle, orgUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return constants.ErrLLMProxyNotFound
		}
		return fmt.Errorf("failed to delete proxy: %w", err)
	}
	return nil
}

// ---- helpers ----

func isSQLiteUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func validateUpstream(u dto.UpstreamConfig) error {
	if u.Main == nil || (u.Main.URL == "" && u.Main.Ref == "") {
		return constants.ErrInvalidInput
	}
	return nil
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func mapExtractionIdentifier(in *dto.ExtractionIdentifier) *model.ExtractionIdentifier {
	if in == nil {
		return nil
	}
	return &model.ExtractionIdentifier{Location: in.Location, Identifier: in.Identifier}
}

func mapAccessControl(in *dto.LLMAccessControl) *model.LLMAccessControl {
	if in == nil {
		return nil
	}
	out := &model.LLMAccessControl{Mode: in.Mode}
	if len(in.Exceptions) > 0 {
		out.Exceptions = make([]model.RouteException, 0, len(in.Exceptions))
		for _, e := range in.Exceptions {
			out.Exceptions = append(out.Exceptions, model.RouteException{Path: e.Path, Methods: e.Methods})
		}
	}
	return out
}

func mapPolicies(in []dto.LLMPolicy) []model.LLMPolicy {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.LLMPolicy, 0, len(in))
	for _, p := range in {
		paths := make([]model.LLMPolicyPath, 0, len(p.Paths))
		for _, pp := range p.Paths {
			paths = append(paths, model.LLMPolicyPath{Path: pp.Path, Methods: pp.Methods, Params: pp.Params})
		}
		out = append(out, model.LLMPolicy{Name: p.Name, Version: p.Version, Paths: paths})
	}
	return out
}

func mapUpstreamAuth(in *dto.UpstreamAuth) *model.UpstreamAuth {
	if in == nil {
		return nil
	}
	return &model.UpstreamAuth{
		Type:   in.Type,
		Header: in.Header,
		Value:  in.Value,
	}
}

func mapUpstreamConfig(in dto.UpstreamConfig) *model.UpstreamConfig {
	out := &model.UpstreamConfig{}
	if in.Main != nil {
		out.Main = &model.UpstreamEndpoint{
			URL: in.Main.URL,
			Ref: in.Main.Ref,
		}
		if in.Main.Auth != nil {
			out.Main.Auth = mapUpstreamAuth(in.Main.Auth)
		}
	}
	if in.Sandbox != nil {
		out.Sandbox = &model.UpstreamEndpoint{
			URL: in.Sandbox.URL,
			Ref: in.Sandbox.Ref,
		}
		if in.Sandbox.Auth != nil {
			out.Sandbox.Auth = mapUpstreamAuth(in.Sandbox.Auth)
		}
	}
	return out
}

func mapUpstreamConfigToDTO(in *model.UpstreamConfig) dto.UpstreamConfig {
	if in == nil {
		return dto.UpstreamConfig{}
	}
	out := dto.UpstreamConfig{}
	if in.Main != nil {
		out.Main = &dto.UpstreamEndpoint{
			URL: in.Main.URL,
			Ref: in.Main.Ref,
		}
		if in.Main.Auth != nil {
			out.Main.Auth = &dto.UpstreamAuth{
				Type:   in.Main.Auth.Type,
				Header: in.Main.Auth.Header,
				Value:  in.Main.Auth.Value,
			}
		}
	}
	if in.Sandbox != nil {
		out.Sandbox = &dto.UpstreamEndpoint{
			URL: in.Sandbox.URL,
			Ref: in.Sandbox.Ref,
		}
		if in.Sandbox.Auth != nil {
			out.Sandbox.Auth = &dto.UpstreamAuth{
				Type:   in.Sandbox.Auth.Type,
				Header: in.Sandbox.Auth.Header,
				Value:  in.Sandbox.Auth.Value,
			}
		}
	}
	return out
}

func mapRateLimiting(in *dto.LLMRateLimitingConfig) *model.LLMRateLimitingConfig {
	if in == nil {
		return nil
	}
	return &model.LLMRateLimitingConfig{
		ProviderLevel: mapRateLimitingScope(in.ProviderLevel),
		ConsumerLevel: mapRateLimitingScope(in.ConsumerLevel),
	}
}

func mapRateLimitingScope(in *dto.RateLimitingScopeConfig) *model.RateLimitingScopeConfig {
	if in == nil {
		return nil
	}
	return &model.RateLimitingScopeConfig{
		Global:       mapRateLimitingLimit(in.Global),
		ResourceWise: mapResourceWiseRateLimiting(in.ResourceWise),
	}
}

func mapRateLimitingLimit(in *dto.RateLimitingLimitConfig) *model.RateLimitingLimitConfig {
	if in == nil {
		return nil
	}
	return &model.RateLimitingLimitConfig{
		Request: mapRequestRateLimit(in.Request),
		Token:   mapTokenRateLimit(in.Token),
		Cost:    mapCostRateLimit(in.Cost),
	}
}

func mapRequestRateLimit(in *dto.RequestRateLimit) *model.RequestRateLimit {
	if in == nil {
		return nil
	}
	return &model.RequestRateLimit{
		Enabled: in.Enabled,
		Count:   in.Count,
		Reset:   mapRateLimitResetWindow(in.Reset),
	}
}

func mapTokenRateLimit(in *dto.TokenRateLimit) *model.TokenRateLimit {
	if in == nil {
		return nil
	}
	return &model.TokenRateLimit{
		Enabled: in.Enabled,
		Count:   in.Count,
		Reset:   mapRateLimitResetWindow(in.Reset),
	}
}

func mapCostRateLimit(in *dto.CostRateLimit) *model.CostRateLimit {
	if in == nil {
		return nil
	}
	return &model.CostRateLimit{
		Enabled: in.Enabled,
		Amount:  in.Amount,
		Reset:   mapRateLimitResetWindow(in.Reset),
	}
}

func mapRateLimitResetWindow(in dto.RateLimitResetWindow) model.RateLimitResetWindow {
	return model.RateLimitResetWindow{
		Duration: in.Duration,
		Unit:     in.Unit,
	}
}

func mapResourceWiseRateLimiting(in *dto.ResourceWiseRateLimitingConfig) *model.ResourceWiseRateLimitingConfig {
	if in == nil {
		return nil
	}
	resources := make([]model.RateLimitingResourceLimit, 0, len(in.Resources))
	for _, r := range in.Resources {
		resources = append(resources, model.RateLimitingResourceLimit{
			Resource: r.Resource,
			Limit:    *mapRateLimitingLimit(&r.Limit),
		})
	}
	return &model.ResourceWiseRateLimitingConfig{
		Default:   *mapRateLimitingLimit(&in.Default),
		Resources: resources,
	}
}

func mapTemplateModelToDTO(m *model.LLMProviderTemplate) *dto.LLMProviderTemplate {
	if m == nil {
		return nil
	}
	return &dto.LLMProviderTemplate{
		ID:               m.ID,
		Name:             m.Name,
		Description:      m.Description,
		CreatedBy:        m.CreatedBy,
		Metadata:         mapTemplateMetadataDTO(m.Metadata),
		PromptTokens:     mapExtractionIdentifierDTO(m.PromptTokens),
		CompletionTokens: mapExtractionIdentifierDTO(m.CompletionTokens),
		TotalTokens:      mapExtractionIdentifierDTO(m.TotalTokens),
		RemainingTokens:  mapExtractionIdentifierDTO(m.RemainingTokens),
		RequestModel:     mapExtractionIdentifierDTO(m.RequestModel),
		ResponseModel:    mapExtractionIdentifierDTO(m.ResponseModel),
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}

func mapTemplateMetadata(in *dto.LLMProviderTemplateMetadata) *model.LLMProviderTemplateMetadata {
	if in == nil {
		return nil
	}
	var auth *model.LLMProviderTemplateAuth
	if in.Auth != nil {
		auth = &model.LLMProviderTemplateAuth{
			Type:        in.Auth.Type,
			Header:      in.Auth.Header,
			ValuePrefix: in.Auth.ValuePrefix,
		}
	}
	out := &model.LLMProviderTemplateMetadata{
		EndpointURL:    strings.TrimSpace(in.EndpointURL),
		Auth:           auth,
		LogoURL:        strings.TrimSpace(in.LogoURL),
		OpenapiSpecURL: strings.TrimSpace(in.OpenapiSpecURL),
	}
	if out.EndpointURL == "" && out.LogoURL == "" && out.Auth == nil && out.OpenapiSpecURL == "" {
		return nil
	}
	return out
}

func mapTemplateMetadataDTO(in *model.LLMProviderTemplateMetadata) *dto.LLMProviderTemplateMetadata {
	if in == nil {
		return nil
	}
	var auth *dto.LLMProviderTemplateAuth
	if in.Auth != nil {
		auth = &dto.LLMProviderTemplateAuth{
			Type:        in.Auth.Type,
			Header:      in.Auth.Header,
			ValuePrefix: in.Auth.ValuePrefix,
		}
	}
	return &dto.LLMProviderTemplateMetadata{
		EndpointURL:    in.EndpointURL,
		Auth:           auth,
		LogoURL:        in.LogoURL,
		OpenapiSpecURL: in.OpenapiSpecURL,
	}
}

func mapExtractionIdentifierDTO(m *model.ExtractionIdentifier) *dto.ExtractionIdentifier {
	if m == nil {
		return nil
	}
	return &dto.ExtractionIdentifier{Location: m.Location, Identifier: m.Identifier}
}

func mapProviderModelToDTO(m *model.LLMProvider, templateHandle string) *dto.LLMProvider {
	if m == nil {
		return nil
	}
	contextValue := ""
	if m.Configuration.Context != nil {
		contextValue = *m.Configuration.Context
	}
	vhostValue := ""
	if m.Configuration.VHost != nil {
		vhostValue = *m.Configuration.VHost
	}
	out := &dto.LLMProvider{
		ID:             m.ID,
		Name:           m.Name,
		Description:    m.Description,
		CreatedBy:      m.CreatedBy,
		Version:        m.Version,
		Context:        contextValue,
		VHost:          vhostValue,
		Template:       templateHandle,
		OpenAPI:        m.OpenAPISpec,
		ModelProviders: mapModelProvidersDTO(m.ModelProviders),
		RateLimiting:   mapRateLimitingDTO(m.Configuration.RateLimiting),
		Upstream:       mapUpstreamConfigToDTO(m.Configuration.Upstream),
		AccessControl:  dto.LLMAccessControl{Mode: "deny_all"},
		Security:       mapSecurityModelToDTO(m.Configuration.Security),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
	if m.Configuration.AccessControl != nil {
		ac := dto.LLMAccessControl{Mode: m.Configuration.AccessControl.Mode}
		if len(m.Configuration.AccessControl.Exceptions) > 0 {
			ac.Exceptions = make([]dto.RouteException, 0, len(m.Configuration.AccessControl.Exceptions))
			for _, e := range m.Configuration.AccessControl.Exceptions {
				ac.Exceptions = append(ac.Exceptions, dto.RouteException{Path: e.Path, Methods: e.Methods})
			}
		}
		out.AccessControl = ac
	}
	if len(m.Configuration.Policies) > 0 {
		out.Policies = make([]dto.LLMPolicy, 0, len(m.Configuration.Policies))
		for _, p := range m.Configuration.Policies {
			paths := make([]dto.LLMPolicyPath, 0, len(p.Paths))
			for _, pp := range p.Paths {
				paths = append(paths, dto.LLMPolicyPath{Path: pp.Path, Methods: pp.Methods, Params: pp.Params})
			}
			out.Policies = append(out.Policies, dto.LLMPolicy{Name: p.Name, Version: p.Version, Paths: paths})
		}
	}
	if out.ModelProviders == nil {
		out.ModelProviders = []dto.LLMModelProvider{}
	}
	if out.Policies == nil {
		out.Policies = []dto.LLMPolicy{}
	}
	if out.AccessControl.Exceptions == nil {
		out.AccessControl.Exceptions = []dto.RouteException{}
	}
	return out
}

func validateModelProviders(template string, providers []dto.LLMModelProvider) error {
	if len(providers) == 0 {
		return nil
	}

	aggregatorTemplates := map[string]bool{
		"awsbedrock":     true,
		"azureaifoundry": true,
	}
	if !aggregatorTemplates[template] && len(providers) > 1 {
		return constants.ErrInvalidInput
	}

	seenProviders := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if p.ID == "" {
			return constants.ErrInvalidInput
		}
		if _, ok := seenProviders[p.ID]; ok {
			return constants.ErrInvalidInput
		}
		seenProviders[p.ID] = struct{}{}

		seenModels := make(map[string]struct{}, len(p.Models))
		for _, m := range p.Models {
			if m.ID == "" {
				return constants.ErrInvalidInput
			}
			if _, ok := seenModels[m.ID]; ok {
				return constants.ErrInvalidInput
			}
			seenModels[m.ID] = struct{}{}
		}
	}
	return nil
}

func mapModelProviders(in []dto.LLMModelProvider) []model.LLMModelProvider {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.LLMModelProvider, 0, len(in))
	for _, p := range in {
		models := make([]model.LLMModel, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, model.LLMModel{
				ID:          m.ID,
				Name:        m.Name,
				Description: m.Description,
			})
		}
		out = append(out, model.LLMModelProvider{
			ID:     p.ID,
			Name:   p.Name,
			Models: models,
		})
	}
	return out
}

func mapModelProvidersDTO(in []model.LLMModelProvider) []dto.LLMModelProvider {
	if len(in) == 0 {
		return nil
	}
	out := make([]dto.LLMModelProvider, 0, len(in))
	for _, p := range in {
		models := make([]dto.LLMModel, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, dto.LLMModel{
				ID:          m.ID,
				Name:        m.Name,
				Description: m.Description,
			})
		}
		out = append(out, dto.LLMModelProvider{
			ID:     p.ID,
			Name:   p.Name,
			Models: models,
		})
	}
	return out
}

func mapRateLimitingDTO(in *model.LLMRateLimitingConfig) *dto.LLMRateLimitingConfig {
	if in == nil {
		return nil
	}
	return &dto.LLMRateLimitingConfig{
		ProviderLevel: mapRateLimitingScopeDTO(in.ProviderLevel),
		ConsumerLevel: mapRateLimitingScopeDTO(in.ConsumerLevel),
	}
}

func mapRateLimitingScopeDTO(in *model.RateLimitingScopeConfig) *dto.RateLimitingScopeConfig {
	if in == nil {
		return nil
	}
	return &dto.RateLimitingScopeConfig{
		Global:       mapRateLimitingLimitDTO(in.Global),
		ResourceWise: mapResourceWiseRateLimitingDTO(in.ResourceWise),
	}
}

func mapRateLimitingLimitDTO(in *model.RateLimitingLimitConfig) *dto.RateLimitingLimitConfig {
	if in == nil {
		return nil
	}
	return &dto.RateLimitingLimitConfig{
		Request: mapRequestRateLimitDTO(in.Request),
		Token:   mapTokenRateLimitDTO(in.Token),
		Cost:    mapCostRateLimitDTO(in.Cost),
	}
}

func mapRequestRateLimitDTO(in *model.RequestRateLimit) *dto.RequestRateLimit {
	if in == nil {
		return nil
	}
	return &dto.RequestRateLimit{
		Enabled: in.Enabled,
		Count:   in.Count,
		Reset:   mapRateLimitResetWindowDTO(in.Reset),
	}
}

func mapTokenRateLimitDTO(in *model.TokenRateLimit) *dto.TokenRateLimit {
	if in == nil {
		return nil
	}
	return &dto.TokenRateLimit{
		Enabled: in.Enabled,
		Count:   in.Count,
		Reset:   mapRateLimitResetWindowDTO(in.Reset),
	}
}

func mapCostRateLimitDTO(in *model.CostRateLimit) *dto.CostRateLimit {
	if in == nil {
		return nil
	}
	return &dto.CostRateLimit{
		Enabled: in.Enabled,
		Amount:  in.Amount,
		Reset:   mapRateLimitResetWindowDTO(in.Reset),
	}
}

func mapRateLimitResetWindowDTO(in model.RateLimitResetWindow) dto.RateLimitResetWindow {
	return dto.RateLimitResetWindow{
		Duration: in.Duration,
		Unit:     in.Unit,
	}
}

func mapResourceWiseRateLimitingDTO(in *model.ResourceWiseRateLimitingConfig) *dto.ResourceWiseRateLimitingConfig {
	if in == nil {
		return nil
	}
	resources := make([]dto.RateLimitingResourceLimit, 0, len(in.Resources))
	for _, r := range in.Resources {
		resources = append(resources, dto.RateLimitingResourceLimit{
			Resource: r.Resource,
			Limit: dto.RateLimitingLimitConfig{
				Request: mapRequestRateLimitDTO(r.Limit.Request),
				Token:   mapTokenRateLimitDTO(r.Limit.Token),
				Cost:    mapCostRateLimitDTO(r.Limit.Cost),
			},
		})
	}
	return &dto.ResourceWiseRateLimitingConfig{
		Default: dto.RateLimitingLimitConfig{
			Request: mapRequestRateLimitDTO(in.Default.Request),
			Token:   mapTokenRateLimitDTO(in.Default.Token),
			Cost:    mapCostRateLimitDTO(in.Default.Cost),
		},
		Resources: resources,
	}
}

func validateRateLimitingConfig(cfg *dto.LLMRateLimitingConfig) error {
	if cfg == nil {
		return nil
	}
	if err := validateRateLimitingScope(cfg.ProviderLevel); err != nil {
		return err
	}
	if err := validateRateLimitingScope(cfg.ConsumerLevel); err != nil {
		return err
	}
	return nil
}

func validateRateLimitingScope(scope *dto.RateLimitingScopeConfig) error {
	if scope == nil {
		return nil
	}
	if (scope.Global == nil && scope.ResourceWise == nil) || (scope.Global != nil && scope.ResourceWise != nil) {
		return constants.ErrInvalidInput
	}
	if scope.Global != nil {
		return validateRateLimitingLimit(scope.Global)
	}
	return validateResourceWiseRateLimiting(scope.ResourceWise)
}

func validateResourceWiseRateLimiting(cfg *dto.ResourceWiseRateLimitingConfig) error {
	if cfg == nil {
		return constants.ErrInvalidInput
	}
	if err := validateRateLimitingLimit(&cfg.Default); err != nil {
		return err
	}
	if len(cfg.Resources) == 0 {
		return constants.ErrInvalidInput
	}
	for _, r := range cfg.Resources {
		if err := validateRateLimitingLimit(&r.Limit); err != nil {
			return err
		}
	}
	return nil
}

func validateRateLimitingLimit(cfg *dto.RateLimitingLimitConfig) error {
	if cfg == nil {
		return constants.ErrInvalidInput
	}
	requestEnabled := cfg.Request != nil && cfg.Request.Enabled
	tokenEnabled := cfg.Token != nil && cfg.Token.Enabled
	costEnabled := cfg.Cost != nil && cfg.Cost.Enabled

	if !requestEnabled && !tokenEnabled && !costEnabled {
		return nil
	}

	if requestEnabled {
		if cfg.Request.Count <= 0 || cfg.Request.Reset.Duration <= 0 {
			return constants.ErrInvalidInput
		}
		if !isValidResetUnit(cfg.Request.Reset.Unit) {
			return constants.ErrInvalidInput
		}
	}
	if tokenEnabled {
		if cfg.Token.Count <= 0 || cfg.Token.Reset.Duration <= 0 {
			return constants.ErrInvalidInput
		}
		if !isValidResetUnit(cfg.Token.Reset.Unit) {
			return constants.ErrInvalidInput
		}
	}
	if costEnabled {
		if cfg.Cost.Amount < 0 || cfg.Cost.Reset.Duration <= 0 {
			return constants.ErrInvalidInput
		}
		if !isValidResetUnit(cfg.Cost.Reset.Unit) {
			return constants.ErrInvalidInput
		}
	}
	return nil
}

func isValidResetUnit(unit string) bool {
	switch unit {
	case "minute", "hour", "day", "week", "month":
		return true
	default:
		return false
	}
}

func mapProxyModelToDTO(m *model.LLMProxy) *dto.LLMProxy {
	if m == nil {
		return nil
	}
	contextValue := ""
	if m.Configuration.Context != nil {
		contextValue = *m.Configuration.Context
	}
	vhostValue := ""
	if m.Configuration.Vhost != nil {
		vhostValue = *m.Configuration.Vhost
	}
	out := &dto.LLMProxy{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		CreatedBy:   m.CreatedBy,
		Version:     m.Version,
		ProjectID:   m.ProjectUUID,
		Context:     contextValue,
		VHost:       vhostValue,
		Provider:    m.Configuration.Provider,
		OpenAPI:     m.OpenAPISpec,
		Security:    mapSecurityModelToDTO(m.Configuration.Security),
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if len(m.Configuration.Policies) > 0 {
		out.Policies = make([]dto.LLMPolicy, 0, len(m.Configuration.Policies))
		for _, p := range m.Configuration.Policies {
			paths := make([]dto.LLMPolicyPath, 0, len(p.Paths))
			for _, pp := range p.Paths {
				paths = append(paths, dto.LLMPolicyPath{Path: pp.Path, Methods: pp.Methods, Params: pp.Params})
			}
			out.Policies = append(out.Policies, dto.LLMPolicy{Name: p.Name, Version: p.Version, Paths: paths})
		}
	}
	if out.Policies == nil {
		out.Policies = []dto.LLMPolicy{}
	}
	return out
}

func mapSecurityDTOToModel(in *dto.SecurityConfig) *model.SecurityConfig {
	if in == nil {
		return nil
	}
	out := &model.SecurityConfig{Enabled: in.Enabled}
	if in.APIKey != nil {
		out.APIKey = &model.APIKeySecurity{Enabled: in.APIKey.Enabled, Key: in.APIKey.Key, In: in.APIKey.In}
	}
	return out
}

func mapSecurityModelToDTO(in *model.SecurityConfig) *dto.SecurityConfig {
	if in == nil {
		return nil
	}
	out := &dto.SecurityConfig{Enabled: in.Enabled}
	if in.APIKey != nil {
		out.APIKey = &dto.APIKeySecurity{Enabled: in.APIKey.Enabled, Key: in.APIKey.Key, In: in.APIKey.In}
	}
	return out
}
