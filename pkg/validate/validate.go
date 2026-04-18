/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package validate

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

var v = validator.New()

// Struct validates s using struct tags and returns the first error encountered,
// or nil if the struct is valid.
func Struct(s any) error {
	return v.Struct(s)
}

// ValidationErrors converts a validator.ValidationErrors value into a map of
// lowercase field name → human-readable error message.
// For any other error type a single "error" key is returned.
func ValidationErrors(err error) map[string]string {
	errs := make(map[string]string)
	if ve, ok := err.(validator.ValidationErrors); ok {
		for _, e := range ve {
			field := strings.ToLower(e.Field())
			errs[field] = fmt.Sprintf("failed validation: %s", e.Tag())
		}
	} else {
		errs["error"] = err.Error()
	}
	return errs
}
