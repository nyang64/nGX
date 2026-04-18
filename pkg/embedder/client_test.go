/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVectorLiteral_Empty(t *testing.T) {
	got := VectorLiteral([]float32{})
	if got != "[]" {
		t.Errorf("want %q, got %q", "[]", got)
	}
}

func TestVectorLiteral_Single(t *testing.T) {
	got := VectorLiteral([]float32{1.5})
	if got != "[1.5]" {
		t.Errorf("want %q, got %q", "[1.5]", got)
	}
}

func TestVectorLiteral_Multiple(t *testing.T) {
	// 1.0 formats as "1", 2.5 as "2.5", 0.1 as "0.1"
	got := VectorLiteral([]float32{1.0, 2.5, 0.1})
	want := "[1,2.5,0.1]"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestEmbed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5}},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", 3)
	vec, err := c.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 dims (truncated), got %d", len(vec))
	}
	// float32 comparison with tolerance
	expected := []float32{0.1, 0.2, 0.3}
	for i, v := range expected {
		if diff := vec[i] - v; diff > 0.0001 || diff < -0.0001 {
			t.Errorf("vec[%d]: want ~%f, got %f", i, v, vec[i])
		}
	}
}

func TestEmbed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", 3)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestEmbed_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", 3)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error to contain %q, got: %v", "empty", err)
	}
}

func TestEmbed_NoDimsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5}},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-model", 0)
	vec, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 5 {
		t.Errorf("expected all 5 dims returned, got %d", len(vec))
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()

	// Pass URL with trailing slash — should not result in //embeddings
	c := New(srv.URL+"/", "test-model", 256)
	_, err := c.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if capturedPath != "/embeddings" {
		t.Errorf("expected path /embeddings, got %q (double slash would be //embeddings)", capturedPath)
	}
}
