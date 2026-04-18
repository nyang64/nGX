/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"net/http"
)

// WSServer is the interface satisfied by server.Hub.ServeWS.
type WSServer interface {
	ServeWS(w http.ResponseWriter, r *http.Request)
}

// WSHandler upgrades HTTP connections to WebSocket.
type WSHandler struct {
	hub WSServer
}

// NewWSHandler creates a WSHandler backed by hub.
func NewWSHandler(hub WSServer) *WSHandler {
	return &WSHandler{hub: hub}
}

// ServeWS upgrades the connection and hands it off to the Hub.
func (h *WSHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	h.hub.ServeWS(w, r)
}
