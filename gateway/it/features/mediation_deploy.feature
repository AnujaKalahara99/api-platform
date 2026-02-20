# --------------------------------------------------------------------
# Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.
# --------------------------------------------------------------------

Feature: MediationApi Deployment
  As an API developer
  I want to deploy a MediationApi configuration
  So that the gateway creates routes for mediation engine entrypoints

  Background:
    Given the gateway services are running

  Scenario: Deploy a MediationApi and verify it is accepted
    Given I authenticate using basic auth as "admin"
    When I deploy this API configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: MediationApi
      metadata:
        name: test-mediation-v1.0
      spec:
        displayName: Test-Mediation
        version: v1.0
        context: /mediation
        entrypoints:
          - name: ws-in
          - name: sse-in
      """
    Then the response should be successful
    And the response should be valid JSON
    And the JSON response field "status" should be "success"

    # Retrieve the deployed MediationApi
    Given I authenticate using basic auth as "admin"
    When I get the API "test-mediation-v1.0"
    Then the response should be successful
    And the response should be valid JSON
    And the JSON response field "api.configuration.kind" should be "MediationApi"
    And the JSON response field "api.configuration.spec.displayName" should be "Test-Mediation"
    And the JSON response field "api.configuration.spec.version" should be "v1.0"
    And the JSON response field "api.configuration.spec.context" should be "/mediation"

    # Cleanup
    Given I authenticate using basic auth as "admin"
    When I delete the API "test-mediation-v1.0"
    Then the response should be successful

  Scenario: Deploy MediationApi with invalid configuration is rejected
    Given I authenticate using basic auth as "admin"
    When I deploy this API configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: MediationApi
      metadata:
        name: invalid-mediation
      spec:
        displayName: ""
        version: invalid
        context: mediation
        entrypoints: []
      """
    Then the response status code should be 400

  Scenario: Update a deployed MediationApi
    Given I authenticate using basic auth as "admin"
    When I deploy this API configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: MediationApi
      metadata:
        name: update-mediation-v1.0
      spec:
        displayName: Update-Mediation
        version: v1.0
        context: /update-mediation
        entrypoints:
          - name: ws-in
      """
    Then the response should be successful

    Given I authenticate using basic auth as "admin"
    When I update the API "update-mediation-v1.0" with this configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: MediationApi
      metadata:
        name: update-mediation-v1.0
      spec:
        displayName: Update-Mediation
        version: v1.0
        context: /update-mediation
        entrypoints:
          - name: ws-in
          - name: sse-in
          - name: http-post
      """
    Then the response should be successful

    # Verify the update
    Given I authenticate using basic auth as "admin"
    When I get the API "update-mediation-v1.0"
    Then the response should be successful
    And the response should be valid JSON
    And the JSON response field "api.configuration.kind" should be "MediationApi"

    # Cleanup
    Given I authenticate using basic auth as "admin"
    When I delete the API "update-mediation-v1.0"
    Then the response should be successful
