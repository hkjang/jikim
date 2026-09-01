package store

import (
	"context"
	"encoding/json"

	"github.com/hkjang/jikim/internal/model"
)

var orderedCapabilities = []string{"create", "read", "update", "delete", "list", "rotate", "encrypt", "decrypt"}

func (s *Store) CapabilitiesForPaths(ctx context.Context, user model.User, paths []string) (map[string][]string, error) {
	result := make(map[string][]string, len(paths))
	for _, path := range paths {
		result[path] = []string{}
	}
	if user.Role == "admin" || user.Role == "manager" {
		for _, path := range paths {
			result[path] = append([]string(nil), orderedCapabilities...)
		}
		return result, nil
	}
	if user.Role == "auditor" {
		for _, path := range paths {
			result[path] = []string{"list"}
		}
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT p.rules FROM policies p
		JOIN user_policies up ON up.policy_id=p.id WHERE up.user_id=$1`, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := make([]model.PathRule, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var policy model.PolicyRules
		if err := json.Unmarshal(raw, &policy); err == nil {
			rules = append(rules, policy.Paths...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, path := range paths {
		granted := make(map[string]bool)
		for _, rule := range rules {
			if !pathMatches(rule.Path, path) {
				continue
			}
			for _, capability := range rule.Capabilities {
				granted[capability] = true
			}
		}
		for _, capability := range orderedCapabilities {
			if granted[capability] {
				result[path] = append(result[path], capability)
			}
		}
	}
	return result, nil
}
