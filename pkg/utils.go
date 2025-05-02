// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// CreateWebSocketConnectURL builds the "connect" URL.
func CreateWebSocketConnectURL(t TunnelTarget, newWebSocket bool) (string, error) {
	if t.Project == "" || t.Port == 0 {
		return "", errors.New("missing required tunnel arguments: project or port")
	}
	u := createWebSocketURL(CONNECT_ENDPOINT, map[string]string{
		"project":      t.Project,
		"port":         strconv.Itoa(t.Port),
		"newWebsocket": fmt.Sprintf("%v", newWebSocket),
		"zone":         t.Zone,
		"instance":     t.Instance,
		"interface":    t.Interface,
		"region":       t.Region,
		"host":         t.Host,
		"group":        t.DestGroup,
		"network":      t.Network,
	}, t.URLOverride)
	return u, nil
}

// CreateWebSocketReconnectURL builds the "reconnect" URL.
func CreateWebSocketReconnectURL(t TunnelTarget, sid uint64, ackBytes uint64, newWebSocket bool) (string, error) {
	u := createWebSocketURL(RECONNECT_ENDPOINT, map[string]string{
		"sid":          fmt.Sprintf("%d", sid),
		"ack":          fmt.Sprintf("%d", ackBytes),
		"newWebsocket": fmt.Sprintf("%v", newWebSocket),
		"zone":         t.Zone,
		"region":       t.Region,
	}, t.URLOverride)
	return u, nil
}

// createWebSocketURL is analogous to _CreateWebSocketUrl in Python code.
func createWebSocketURL(endpoint string, query map[string]string, urlOverride string) string {
	useMTLS := false // Omit context-aware logic
	scheme := URL_SCHEME
	host := URL_HOST
	if useMTLS {
		host = MTLS_URL_HOST
	}
	pathRoot := URL_PATH_ROOT
	// If user provided an override
	if urlOverride != "" {
		parsed, err := url.Parse(urlOverride)
		if err == nil {
			if parsed.Scheme != "" {
				scheme = parsed.Scheme
			}
			if parsed.Host != "" {
				host = parsed.Host
			}
			if parsed.Path != "" && parsed.Path != "/" {
				pathRoot = parsed.Path
			}
		}
	}
	v := url.Values{}
	for k, val := range query {
		if val != "" {
			v.Add(k, val)
		}
	}
	thePath := pathRoot
	if thePath != "" && thePath[len(thePath)-1] != '/' {
		thePath += "/"
	}
	thePath += endpoint
	u := url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     thePath,
		RawQuery: v.Encode(),
	}
	return u.String()
}

// Helper extracting a 32-bit int from data.
func extractUint32(data []byte) (uint32, []byte, error) {
	if len(data) < 4 {
		return 0, nil, errors.New("incomplete data for uint32")
	}
	val := binary.BigEndian.Uint32(data[:4])
	return val, data[4:], nil
}
