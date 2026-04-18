/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package server

import (
	authpkg "agentmail/pkg/auth"
	"agentmail/services/api/clients"
	"agentmail/services/api/handlers"

	"github.com/go-chi/chi/v5"
)

// registerRoutes wires all API routes onto r.
func registerRoutes(
	r *chi.Mux,
	authClient *clients.AuthClient,
	healthH *handlers.HealthHandler,
	orgH *handlers.OrgHandler,
	inboxH *handlers.InboxHandler,
	threadH *handlers.ThreadHandler,
	messageH *handlers.MessageHandler,
	draftH *handlers.DraftHandler,
	labelH *handlers.LabelHandler,
	webhookH *handlers.WebhookHandler,
	apiKeyH *handlers.APIKeyHandler,
	searchH *handlers.SearchHandler,
	wsH *handlers.WSHandler,
) {
	auth := authMiddleware(authClient)

	// Public routes.
	r.Get("/health", healthH.Health)
	r.Get("/readyz", healthH.Ready)

	// All v1 routes require a valid API key.
	r.Route("/v1", func(r chi.Router) {
		r.Use(auth)

		// WebSocket — auth middleware already applied.
		r.Get("/ws", wsH.ServeWS)

		// Organization (self-service: an authenticated caller reads their own org).
		r.Route("/org", func(r chi.Router) {
			r.Use(requireScope(authpkg.ScopeOrgAdmin))
			r.Get("/", orgH.GetOrg)
			r.Patch("/", orgH.UpdateOrg)
		})

		// Pods.
		r.Route("/pods", func(r chi.Router) {
			r.Use(requireScope(authpkg.ScopePodAdmin))
			r.Get("/", orgH.ListPods)
			r.Post("/", orgH.CreatePod)
			r.Route("/{podID}", func(r chi.Router) {
				r.Get("/", orgH.GetPod)
				r.Patch("/", orgH.UpdatePod)
				r.Delete("/", orgH.DeletePod)
			})
		})

		// API keys — proxied to auth service.
		r.Route("/keys", func(r chi.Router) {
			r.Use(requireScope(authpkg.ScopeOrgAdmin))
			r.Get("/", apiKeyH.ListKeys)
			r.Post("/", apiKeyH.CreateKey)
			r.Route("/{keyID}", func(r chi.Router) {
				r.Get("/", apiKeyH.GetKey)
				r.Delete("/", apiKeyH.RevokeKey)
			})
		})

		// Inboxes.
		r.Route("/inboxes", func(r chi.Router) {
			r.With(requireScope(authpkg.ScopeInboxRead)).Get("/", inboxH.ListInboxes)
			r.With(requireScope(authpkg.ScopeInboxWrite)).Post("/", inboxH.CreateInbox)

			r.Route("/{inboxID}", func(r chi.Router) {
				r.With(requireScope(authpkg.ScopeInboxRead)).Get("/", inboxH.GetInbox)
				r.With(requireScope(authpkg.ScopeInboxWrite)).Patch("/", inboxH.UpdateInbox)
				r.With(requireScope(authpkg.ScopeInboxWrite)).Delete("/", inboxH.DeleteInbox)

				// Threads.
				r.Route("/threads", func(r chi.Router) {
					r.With(requireScope(authpkg.ScopeInboxRead)).Get("/", threadH.ListThreads)

					r.Route("/{threadID}", func(r chi.Router) {
						r.With(requireScope(authpkg.ScopeInboxRead)).Get("/", threadH.GetThread)
						r.With(requireScope(authpkg.ScopeInboxWrite)).Patch("/", threadH.UpdateThread)

						// Messages.
						r.Route("/messages", func(r chi.Router) {
							r.With(requireScope(authpkg.ScopeInboxRead)).Get("/", messageH.ListMessages)
							r.With(requireScope(authpkg.ScopeInboxRead)).Get("/{messageID}", messageH.GetMessage)
						})

						// Thread labels.
						r.Route("/labels", func(r chi.Router) {
							r.With(requireScope(authpkg.ScopeInboxWrite)).Put("/{labelID}", threadH.ApplyLabel)
							r.With(requireScope(authpkg.ScopeInboxWrite)).Delete("/{labelID}", threadH.RemoveLabel)
						})
					})
				})

				// Drafts.
				r.Route("/drafts", func(r chi.Router) {
					r.With(requireScope(authpkg.ScopeDraftRead)).Get("/", draftH.ListDrafts)
					r.With(requireScope(authpkg.ScopeDraftWrite)).Post("/", draftH.CreateDraft)

					r.Route("/{draftID}", func(r chi.Router) {
						r.With(requireScope(authpkg.ScopeDraftRead)).Get("/", draftH.GetDraft)
						r.With(requireScope(authpkg.ScopeDraftWrite)).Patch("/", draftH.UpdateDraft)
						r.With(requireScope(authpkg.ScopeDraftWrite)).Delete("/", draftH.DeleteDraft)
						r.With(requireScope(authpkg.ScopeDraftWrite)).Post("/approve", draftH.ApproveDraft)
						r.With(requireScope(authpkg.ScopeDraftWrite)).Post("/reject", draftH.RejectDraft)
					})
				})

				// Send a message directly (not a draft).
				r.With(requireScope(authpkg.ScopeInboxWrite)).Post("/messages/send", messageH.SendMessage)
			})
		})

		// Labels (org-scoped).
		r.Route("/labels", func(r chi.Router) {
			r.With(requireScope(authpkg.ScopeInboxRead)).Get("/", labelH.ListLabels)
			r.With(requireScope(authpkg.ScopeInboxWrite)).Post("/", labelH.CreateLabel)
			r.Route("/{labelID}", func(r chi.Router) {
				r.With(requireScope(authpkg.ScopeInboxWrite)).Patch("/", labelH.UpdateLabel)
				r.With(requireScope(authpkg.ScopeInboxWrite)).Delete("/", labelH.DeleteLabel)
			})
		})

		// Webhooks — proxied to webhook service.
		r.Route("/webhooks", func(r chi.Router) {
			r.With(requireScope(authpkg.ScopeWebhookRead)).Get("/", webhookH.ListWebhooks)
			r.With(requireScope(authpkg.ScopeWebhookWrite)).Post("/", webhookH.CreateWebhook)
			r.Route("/{webhookID}", func(r chi.Router) {
				r.With(requireScope(authpkg.ScopeWebhookRead)).Get("/", webhookH.GetWebhook)
				r.With(requireScope(authpkg.ScopeWebhookWrite)).Patch("/", webhookH.UpdateWebhook)
				r.With(requireScope(authpkg.ScopeWebhookWrite)).Delete("/", webhookH.DeleteWebhook)
				r.With(requireScope(authpkg.ScopeWebhookRead)).Get("/deliveries", webhookH.ListDeliveries)
			})
		})

		// Search.
		r.With(requireScope(authpkg.ScopeSearchRead)).Get("/search", searchH.Search)
	})
}
