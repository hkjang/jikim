package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hkjang/jikim/internal/model"
)

type PolicySimulationMatch struct {
	PolicyID     string   `json:"policy_id"`
	PolicyName   string   `json:"policy_name"`
	RulePath     string   `json:"rule_path"`
	Capabilities []string `json:"capabilities"`
	Grants       bool     `json:"grants"`
}

type PolicySimulation struct {
	Allowed      bool                    `json:"allowed"`
	UserID       string                  `json:"user_id"`
	Username     string                  `json:"username"`
	Path         string                  `json:"path"`
	Capability   string                  `json:"capability"`
	RoleOverride bool                    `json:"role_override"`
	Matches      []PolicySimulationMatch `json:"matches"`
}

func (s *Store) SimulateAccess(ctx context.Context, user model.User, path, capability string) (PolicySimulation, error) {
	path = strings.Trim(path, "/")
	if err := ValidateSecretPath(path); err != nil {
		return PolicySimulation{}, err
	}
	if !validCapability(capability) {
		return PolicySimulation{}, fmt.Errorf("%w: 지원하지 않는 capability", ErrInvalid)
	}
	result := PolicySimulation{UserID: user.ID, Username: user.Username, Path: path,
		Capability: capability, Matches: make([]PolicySimulationMatch, 0)}
	if !user.Active {
		return result, nil
	}
	switch user.Role {
	case "admin", "manager":
		result.Allowed = true
		result.RoleOverride = true
		return result, nil
	case "auditor":
		result.Allowed = capability == "list"
		result.RoleOverride = result.Allowed
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id,p.name,p.rules FROM policies p
		JOIN user_policies up ON up.policy_id=p.id WHERE up.user_id=$1 ORDER BY p.name`, user.ID)
	if err != nil {
		return PolicySimulation{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var policyID, name string
		var raw []byte
		if err := rows.Scan(&policyID, &name, &raw); err != nil {
			return PolicySimulation{}, err
		}
		var rules model.PolicyRules
		if err := json.Unmarshal(raw, &rules); err != nil {
			continue
		}
		for _, rule := range rules.Paths {
			if !pathMatches(rule.Path, path) {
				continue
			}
			match := PolicySimulationMatch{PolicyID: policyID, PolicyName: name,
				RulePath: rule.Path, Capabilities: append([]string(nil), rule.Capabilities...)}
			for _, allowed := range rule.Capabilities {
				if allowed == capability {
					match.Grants = true
					result.Allowed = true
				}
			}
			result.Matches = append(result.Matches, match)
		}
	}
	return result, rows.Err()
}

func validCapability(value string) bool {
	switch value {
	case "create", "read", "update", "delete", "list", "rotate", "encrypt", "decrypt":
		return true
	default:
		return false
	}
}
