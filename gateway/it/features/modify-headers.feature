Feature: Modify Headers Policy Integration Tests
  Test the modify-headers policy for comprehensive header manipulation in request and response flows

  Background:
    Given the gateway services are running
    And I authenticate using basic auth as "admin"

  # ========================================
  # Request Header Modifications
  # ========================================

  Scenario: Set a single request header
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-set-request
      spec:
        displayName: Modify-Headers-Set-Request-Test
        version: v1.0.0
        context: /modify-headers-set-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Custom-Header
                      value: CustomValue
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-set-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-set-req/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-custom-header" with value "CustomValue"

  Scenario: Append values to a request header
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-append-request
      spec:
        displayName: Modify-Headers-Append-Request-Test
        version: v1.0.0
        context: /modify-headers-append-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: APPEND
                      name: X-Forwarded-For
                      value: gateway-proxy-1
                    - action: APPEND
                      name: X-Forwarded-For
                      value: gateway-proxy-2
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-append-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-append-req/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-forwarded-for" with both values "gateway-proxy-1" and "gateway-proxy-2"

  Scenario: Delete a request header
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-delete-request
      spec:
        displayName: Modify-Headers-Delete-Request-Test
        version: v1.0.0
        context: /modify-headers-delete-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: DELETE
                      name: User-Agent
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-delete-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-delete-req/v1.0.0/test" with header "User-Agent: TestClient/1.0"
    Then the response status code should be 200
    And the response should not contain echoed header "user-agent"

  Scenario: Multiple request header operations (SET, APPEND, DELETE)
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-multiple-request
      spec:
        displayName: Modify-Headers-Multiple-Request-Test
        version: v1.0.0
        context: /modify-headers-multiple-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Request-ID
                      value: req-12345
                    - action: APPEND
                      name: X-Tracking
                      value: tracker-1
                    - action: APPEND
                      name: X-Tracking
                      value: tracker-2
                    - action: DELETE
                      name: X-Internal-Token
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-multiple-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-multiple-req/v1.0.0/test" with header "X-Internal-Token: secret-token"
    Then the response status code should be 200
    And the response should contain echoed header "x-request-id" with value "req-12345"
    And the response should contain echoed header "x-tracking" with both values "tracker-1" and "tracker-2"
    And the response should not contain echoed header "x-internal-token"

  Scenario: SET replaces existing request header value
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-replace-request
      spec:
        displayName: Modify-Headers-Replace-Request-Test
        version: v1.0.0
        context: /modify-headers-replace-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: Authorization
                      value: Bearer new-token
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-replace-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-replace-req/v1.0.0/test" with header "Authorization: Bearer old-token"
    Then the response status code should be 200
    And the response should contain echoed header "authorization" with value "Bearer new-token"

  Scenario: Header names are case-insensitive in request modifications
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-case-insensitive-req
      spec:
        displayName: Modify-Headers-Case-Insensitive-Request-Test
        version: v1.0.0
        context: /modify-headers-case-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Custom-HEADER
                      value: test-value
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-case-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-case-req/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-custom-header" with value "test-value"

  # ========================================
  # Response Header Modifications
  # ========================================

  Scenario: Set a single response header
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-set-response
      spec:
        displayName: Modify-Headers-Set-Response-Test
        version: v1.0.0
        context: /modify-headers-set-resp/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: SET
                      name: X-Gateway-Response
                      value: processed
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-set-resp/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-set-resp/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "X-Gateway-Response" with value "processed"

  Scenario: Append values to a response header
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-append-response
      spec:
        displayName: Modify-Headers-Append-Response-Test
        version: v1.0.0
        context: /modify-headers-append-resp/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: APPEND
                      name: X-Gateway-Chain
                      value: gateway-1
                    - action: APPEND
                      name: X-Gateway-Chain
                      value: gateway-2
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-append-resp/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-append-resp/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "X-Gateway-Chain" with values "gateway-1" and "gateway-2"

  Scenario: Delete a response header
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-delete-response
      spec:
        displayName: Modify-Headers-Delete-Response-Test
        version: v1.0.0
        context: /modify-headers-delete-resp/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: DELETE
                      name: X-Echo-Response
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-delete-resp/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-delete-resp/v1.0.0/test"
    Then the response status code should be 200
    And the response should not have header "X-Echo-Response"

  Scenario: Multiple response header operations (SET, APPEND, DELETE)
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-multiple-response
      spec:
        displayName: Modify-Headers-Multiple-Response-Test
        version: v1.0.0
        context: /modify-headers-multiple-resp/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: SET
                      name: X-Response-ID
                      value: resp-67890
                    - action: APPEND
                      name: X-Process-Chain
                      value: step-1
                    - action: APPEND
                      name: X-Process-Chain
                      value: step-2
                    - action: DELETE
                      name: X-Internal-Debug
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-multiple-resp/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-multiple-resp/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "X-Response-ID" with value "resp-67890"
    And the response should have header "X-Process-Chain" with values "step-1" and "step-2"
    And the response should not have header "X-Internal-Debug"

  # ========================================
  # Combined Request and Response Modifications
  # ========================================

  Scenario: Modify both request and response headers
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-both
      spec:
        displayName: Modify-Headers-Both-Test
        version: v1.0.0
        context: /modify-headers-both/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: POST
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Request-Modified
                      value: "true"
                    - action: DELETE
                      name: X-Client-Secret
                  responseHeaders:
                    - action: SET
                      name: X-Response-Modified
                      value: "true"
                    - action: DELETE
                      name: X-Backend-Internal
      """
    Then the response should be successful
    And I wait for 3 seconds
    When I send a POST request to "http://localhost:8080/modify-headers-both/v1.0.0/test" with header "X-Client-Secret: secret123" and body:
      """
      {"test": "data"}
      """
    Then the response status code should be 200
    And the response should contain echoed header "x-request-modified" with value "true"
    And the response should not contain echoed header "x-client-secret"
    And the response should have header "X-Response-Modified" with value "true"

  # ========================================
  # Security Headers Use Case
  # ========================================

  Scenario: Add security headers to response
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-security
      spec:
        displayName: Modify-Headers-Security-Test
        version: v1.0.0
        context: /modify-headers-security/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: SET
                      name: X-Frame-Options
                      value: DENY
                    - action: SET
                      name: X-Content-Type-Options
                      value: nosniff
                    - action: SET
                      name: Strict-Transport-Security
                      value: max-age=31536000
                    - action: SET
                      name: X-XSS-Protection
                      value: 1; mode=block
                    - action: DELETE
                      name: Server
                    - action: DELETE
                      name: X-Powered-By
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-security/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-security/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "X-Frame-Options" with value "DENY"
    And the response should have header "X-Content-Type-Options" with value "nosniff"
    And the response should have header "Strict-Transport-Security" with value "max-age=31536000"
    And the response should have header "X-XSS-Protection" with value "1; mode=block"

  # ========================================
  # CORS Headers Use Case
  # ========================================

  Scenario: Configure CORS headers on response
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-cors
      spec:
        displayName: Modify-Headers-CORS-Test
        version: v1.0.0
        context: /modify-headers-cors/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: SET
                      name: Access-Control-Allow-Origin
                      value: https://example.com
                    - action: SET
                      name: Access-Control-Allow-Methods
                      value: GET, POST, PUT, DELETE
                    - action: APPEND
                      name: Access-Control-Allow-Headers
                      value: Authorization
                    - action: APPEND
                      name: Access-Control-Allow-Headers
                      value: Content-Type
                    - action: SET
                      name: Access-Control-Max-Age
                      value: "3600"
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-cors/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-cors/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "Access-Control-Allow-Origin" with value "https://example.com"
    And the response should have header "Access-Control-Allow-Methods" with value "GET, POST, PUT, DELETE"
    And the response should have header "Access-Control-Allow-Headers" with values "Authorization" and "Content-Type"
    And the response should have header "Access-Control-Max-Age" with value "3600"

  # ========================================
  # Rate Limit Headers Use Case
  # ========================================

  Scenario: Add rate limit information to response headers
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-ratelimit
      spec:
        displayName: Modify-Headers-RateLimit-Test
        version: v1.0.0
        context: /modify-headers-ratelimit/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: SET
                      name: X-RateLimit-Limit
                      value: "1000"
                    - action: SET
                      name: X-RateLimit-Remaining
                      value: "999"
                    - action: SET
                      name: X-RateLimit-Reset
                      value: "1640995200"
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-ratelimit/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-ratelimit/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "X-RateLimit-Limit" with value "1000"
    And the response should have header "X-RateLimit-Remaining" with value "999"
    And the response should have header "X-RateLimit-Reset" with value "1640995200"

  # ========================================
  # Edge Cases
  # ========================================

  Scenario: SET with empty value creates header with empty string
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-empty-value
      spec:
        displayName: Modify-Headers-Empty-Value-Test
        version: v1.0.0
        context: /modify-headers-empty/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Empty-Header
                      value: ""
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-empty/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-empty/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-empty-header" with value ""

  Scenario: DELETE non-existent header does not cause error
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-delete-nonexistent
      spec:
        displayName: Modify-Headers-Delete-NonExistent-Test
        version: v1.0.0
        context: /modify-headers-delete-none/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: DELETE
                      name: X-Does-Not-Exist
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-delete-none/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-delete-none/v1.0.0/test"
    Then the response status code should be 200

  Scenario: Header value with special characters
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-special-chars
      spec:
        displayName: Modify-Headers-Special-Chars-Test
        version: v1.0.0
        context: /modify-headers-special/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Special-Value
                      value: "value with spaces, commas; semicolons: colons = equals"
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-special/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-special/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-special-value" with value "value with spaces, commas; semicolons: colons = equals"

  Scenario: Very long header value
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-long-value
      spec:
        displayName: Modify-Headers-Long-Value-Test
        version: v1.0.0
        context: /modify-headers-long/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Long-Value
                      value: "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum. Sed ut perspiciatis unde omnis iste natus error sit voluptatem accusantium doloremque laudantium, totam rem aperiam, eaque ipsa quae ab illo inventore veritatis et quasi architecto beatae vitae dicta sunt explicabo."
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-long/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-long/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-long-value" containing "Lorem ipsum dolor sit amet"

  Scenario: Only request headers specified (no response headers)
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-only-request
      spec:
        displayName: Modify-Headers-Only-Request-Test
        version: v1.0.0
        context: /modify-headers-only-req/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Only-Request
                      value: test-value
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-only-req/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-only-req/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-only-request" with value "test-value"

  Scenario: Only response headers specified (no request headers)
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-only-response
      spec:
        displayName: Modify-Headers-Only-Response-Test
        version: v1.0.0
        context: /modify-headers-only-resp/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  responseHeaders:
                    - action: SET
                      name: X-Only-Response
                      value: test-value
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-only-resp/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-only-resp/v1.0.0/test"
    Then the response status code should be 200
    And the response should have header "X-Only-Response" with value "test-value"

  # ========================================
  # Custom Tracking Headers Use Case
  # ========================================

  Scenario: Add custom tracking and correlation headers
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-tracking
      spec:
        displayName: Modify-Headers-Tracking-Test
        version: v1.0.0
        context: /modify-headers-tracking/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: POST
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Correlation-ID
                      value: corr-abc123
                    - action: SET
                      name: X-Gateway-Version
                      value: v2.0.0
                    - action: APPEND
                      name: X-Route-Path
                      value: gateway
                    - action: APPEND
                      name: X-Route-Path
                      value: policy-engine
                  responseHeaders:
                    - action: SET
                      name: X-Response-Time
                      value: 45ms
                    - action: SET
                      name: X-Gateway-ID
                      value: gateway-instance-1
      """
    Then the response should be successful
    And I wait for 3 seconds
    When I send a POST request to "http://localhost:8080/modify-headers-tracking/v1.0.0/test" with body:
      """
      {"transaction": "payment"}
      """
    Then the response status code should be 200
    And the response should contain echoed header "x-correlation-id" with value "corr-abc123"
    And the response should contain echoed header "x-gateway-version" with value "v2.0.0"
    And the response should contain echoed header "x-route-path" with both values "gateway" and "policy-engine"
    And the response should have header "X-Response-Time" with value "45ms"
    And the response should have header "X-Gateway-ID" with value "gateway-instance-1"

  # ========================================
  # Header Name with Underscores and Hyphens
  # ========================================

  Scenario: Header names with underscores and hyphens
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-names
      spec:
        displayName: Modify-Headers-Names-Test
        version: v1.0.0
        context: /modify-headers-names/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Custom_Header-123
                      value: test-value-1
                    - action: SET
                      name: X_Another-Custom_Header
                      value: test-value-2
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-names/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-names/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-custom_header-123" with value "test-value-1"
    And the response should contain echoed header "x_another-custom_header" with value "test-value-2"

  # ========================================
  # Multiple SET operations on same header (last wins)
  # ========================================

  Scenario: Multiple SET operations on same header - last one wins
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-multiple-set
      spec:
        displayName: Modify-Headers-Multiple-SET-Test
        version: v1.0.0
        context: /modify-headers-multi-set/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Test-Header
                      value: first-value
                    - action: SET
                      name: X-Test-Header
                      value: second-value
                    - action: SET
                      name: X-Test-Header
                      value: final-value
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-multi-set/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-multi-set/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-test-header" with value "final-value"

  # ========================================
  # Content-Type Preservation
  # ========================================

  Scenario: Modify headers while preserving content-type
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-content-type
      spec:
        displayName: Modify-Headers-Content-Type-Test
        version: v1.0.0
        context: /modify-headers-content/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: POST
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-Custom-Request
                      value: modified
                  responseHeaders:
                    - action: SET
                      name: X-Custom-Response
                      value: processed
      """
    Then the response should be successful
    And I wait for 3 seconds
    When I send a POST request to "http://localhost:8080/modify-headers-content/v1.0.0/test" with header "Content-Type: application/json" and body:
      """
      {"message": "test"}
      """
    Then the response status code should be 200
    And the response should have header "Content-Type" containing "application/json"
    And the response should have header "X-Custom-Response" with value "processed"

  # ========================================
  # Authorization Header Modification
  # ========================================

  Scenario: Replace authorization header for backend
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-auth
      spec:
        displayName: Modify-Headers-Auth-Test
        version: v1.0.0
        context: /modify-headers-auth/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: Authorization
                      value: Bearer backend-service-token-xyz
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-auth/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-auth/v1.0.0/test" with header "Authorization: Bearer client-token-abc"
    Then the response status code should be 200
    And the response should contain echoed header "authorization" with value "Bearer backend-service-token-xyz"

  # ========================================
  # API Versioning Headers
  # ========================================

  Scenario: Add API version headers to request and response
    When I deploy an API with the following configuration:
      """
      apiVersion: gateway.api-platform.wso2.com/v1alpha1
      kind: RestApi
      metadata:
        name: test-modify-headers-versioning
      spec:
        displayName: Modify-Headers-Versioning-Test
        version: v1.0.0
        context: /modify-headers-version/$version
        upstream:
          main:
            url: http://sample-backend:9080/echo
        operations:
          - method: GET
            path: /test
            policies:
              - name: modify-headers
                version: v0
                params:
                  requestHeaders:
                    - action: SET
                      name: X-API-Version
                      value: v1.0.0
                    - action: SET
                      name: X-Backend-Version
                      value: v2.5.0
                  responseHeaders:
                    - action: SET
                      name: X-Gateway-Version
                      value: v1.2.3
                    - action: SET
                      name: X-API-Deprecated
                      value: "false"
      """
    Then the response should be successful
    And I wait for the endpoint "http://localhost:8080/modify-headers-version/v1.0.0/test" to be ready
    When I send a GET request to "http://localhost:8080/modify-headers-version/v1.0.0/test"
    Then the response status code should be 200
    And the response should contain echoed header "x-api-version" with value "v1.0.0"
    And the response should contain echoed header "x-backend-version" with value "v2.5.0"
    And the response should have header "X-Gateway-Version" with value "v1.2.3"
    And the response should have header "X-API-Deprecated" with value "false"
