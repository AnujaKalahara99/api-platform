package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// ClientIDHeader is the response header for client identifier (like X-Gravitee-Client-Identifier)
	ClientIDHeader = "X-WSO2-Client-Identifier"
	// ClientIDRequestHeader allows clients to provide their own identifier
	ClientIDRequestHeader = "X-WSO2-Client-ID"
)

// ClientIdentity represents a uniquely identified client connection
type ClientIdentity struct {
	ID          string    `json:"id"`
	RemoteAddr  string    `json:"remote_addr"`
	ProvidedID  string    `json:"provided_id,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`
	Protocol    string    `json:"protocol"`
	SourceName  string    `json:"source_name"`
}

// ExtractClientIdentity extracts or generates a ClientIdentity from an HTTP request
func ExtractClientIdentity(r *http.Request, protocol, sourceName string) *ClientIdentity {
	remoteAddr := extractRemoteAddr(r)
	providedID := r.Header.Get(ClientIDRequestHeader)

	var clientID string
	if providedID != "" {
		clientID = providedID
	} else {
		clientID = generateIdentifier(remoteAddr, r)
	}

	return &ClientIdentity{
		ID:          clientID,
		RemoteAddr:  remoteAddr,
		ProvidedID:  providedID,
		ConnectedAt: time.Now().UTC(),
		Protocol:    protocol,
		SourceName:  sourceName,
	}
}

func extractRemoteAddr(r *http.Request) string {
	// Check X-Forwarded-For (set by Envoy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func generateIdentifier(remoteAddr string, r *http.Request) string {
	data := fmt.Sprintf("%s|%s|%s|%d",
		remoteAddr,
		r.Header.Get("User-Agent"),
		r.URL.Path,
		// time.Now().UnixNano(),
	)
	return hash(data)
}

// func hashIdentifier(providedID string) string {
// 	data := fmt.Sprintf("%s", providedID)
// 	return hash(data)
// }

func hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}
