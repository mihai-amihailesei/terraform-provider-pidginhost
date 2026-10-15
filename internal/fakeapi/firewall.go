// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"net/http"
	"strings"
)

type ruleSet struct {
	ID    int32
	Name  string
	Rules map[int32]*rule
}

type rule struct {
	ID          int32
	Direction   string
	Action      string
	Protocol    string
	Source      string
	Sport       string
	Destination string
	Dport       string
	Enabled     bool
	Position    int
}

func (r *rule) validate() (bool, string) {
	if r.Protocol == "" && (r.Sport != "" || r.Dport != "") {
		return true, "Ports can only be used together with a protocol."
	}
	return false, ""
}

func (r *rule) json() map[string]any {
	hasErr, msg := r.validate()
	return map[string]any{
		"id": r.ID, "direction": r.Direction, "action": r.Action, "protocol": r.Protocol,
		"source": r.Source, "sport": r.Sport, "destination": r.Destination, "dport": r.Dport,
		"enabled": r.Enabled, "position": r.Position, "has_error": hasErr, "error_message": msg,
	}
}

func (s *ruleSet) json() map[string]any {
	rules := []any{}
	status := "validated"
	for _, id := range sortedKeys(s.Rules) {
		rj := s.Rules[id].json()
		if rj["has_error"].(bool) {
			status = "invalid"
		}
		rules = append(rules, rj)
	}
	return map[string]any{"id": s.ID, "name": s.Name, "status": status, "rules": rules, "read_only": false}
}

func (r *rule) patch(b map[string]any) (field, msg string) {
	if v, ok := str(b, "direction"); ok {
		if v != "in" && v != "out" {
			return "direction", `"` + v + `" is not a valid choice.`
		}
		r.Direction = v
	}
	if v, ok := str(b, "action"); ok {
		if v != "ACCEPT" && v != "DROP" && v != "REJECT" {
			return "action", `"` + v + `" is not a valid choice.`
		}
		r.Action = v
	}
	for key, dst := range map[string]*string{
		"protocol": &r.Protocol, "source": &r.Source, "sport": &r.Sport,
		"destination": &r.Destination, "dport": &r.Dport,
	} {
		if v, ok := str(b, key); ok {
			*dst = v
		}
	}
	if v, ok := boolean(b, "enabled"); ok {
		r.Enabled = v
	}
	if v, ok := integer(b, "position"); ok {
		r.Position = v
	}
	return "", ""
}

// RuleSetIDs returns the IDs of all firewall rule sets.
func (f *Server) RuleSetIDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.ruleSets)
}

// RenameRuleSet renames a rule set out of band.
func (f *Server) RenameRuleSet(id int32, name string) {
	f.Do(func() { f.ruleSets[id].Name = name })
}

// SetRuleDport changes the dport of every rule of a set out of band.
func (f *Server) SetRuleDport(setID int32, dport string) {
	f.Do(func() {
		for _, r := range f.ruleSets[setID].Rules {
			r.Dport = dport
		}
	})
}

func (f *Server) firewallRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cloud/firewall-rules-set/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []any{}
		for _, id := range sortedKeys(f.ruleSets) {
			items = append(items, f.ruleSets[id].json())
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("POST /api/cloud/firewall-rules-set/{$}", func(w http.ResponseWriter, r *http.Request) {
		name, _ := str(bodyOf(r), "name")
		if strings.TrimSpace(name) == "" {
			badRequest(w, "name", "This field is required.")
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		s := &ruleSet{ID: f.id(), Name: name, Rules: map[int32]*rule{}}
		f.ruleSets[s.ID] = s
		writeJSON(w, http.StatusCreated, s.json())
	})
	mux.HandleFunc("GET /api/cloud/firewall-rules-set/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRuleSet(w, r, "id", func(s *ruleSet) { writeJSON(w, http.StatusOK, s.json()) })
	})
	mux.HandleFunc("PATCH /api/cloud/firewall-rules-set/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRuleSet(w, r, "id", func(s *ruleSet) {
			if n, ok := str(bodyOf(r), "name"); ok {
				s.Name = n
			}
			writeJSON(w, http.StatusOK, s.json())
		})
	})
	mux.HandleFunc("DELETE /api/cloud/firewall-rules-set/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRuleSet(w, r, "id", func(s *ruleSet) {
			if f.ruleSetInUse(s.ID) {
				badRequest(w, "detail", "The rules set is in use by a server.")
				return
			}
			delete(f.ruleSets, s.ID)
			w.WriteHeader(http.StatusNoContent)
		})
	})

	mux.HandleFunc("GET /api/cloud/firewall-rules-set/{rules_set_id}/rules/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRuleSet(w, r, "rules_set_id", func(s *ruleSet) {
			items := []any{}
			for _, id := range sortedKeys(s.Rules) {
				items = append(items, s.Rules[id].json())
			}
			writeJSON(w, http.StatusOK, items)
		})
	})
	mux.HandleFunc("POST /api/cloud/firewall-rules-set/{rules_set_id}/rules/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRuleSet(w, r, "rules_set_id", func(s *ruleSet) {
			b := bodyOf(r)
			if _, ok := str(b, "direction"); !ok {
				badRequest(w, "direction", "This field is required.")
				return
			}
			if _, ok := str(b, "action"); !ok {
				badRequest(w, "action", "This field is required.")
				return
			}
			ru := &rule{ID: f.id(), Enabled: true, Position: len(s.Rules) + 1}
			if field, msg := ru.patch(b); field != "" {
				badRequest(w, field, msg)
				return
			}
			s.Rules[ru.ID] = ru
			writeJSON(w, http.StatusCreated, ru.json())
		})
	})
	mux.HandleFunc("GET /api/cloud/firewall-rules-set/{rules_set_id}/rules/{rule_id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRule(w, r, func(_ *ruleSet, ru *rule) { writeJSON(w, http.StatusOK, ru.json()) })
	})
	mux.HandleFunc("PATCH /api/cloud/firewall-rules-set/{rules_set_id}/rules/{rule_id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRule(w, r, func(_ *ruleSet, ru *rule) {
			if field, msg := ru.patch(bodyOf(r)); field != "" {
				badRequest(w, field, msg)
				return
			}
			writeJSON(w, http.StatusOK, ru.json())
		})
	})
	mux.HandleFunc("DELETE /api/cloud/firewall-rules-set/{rules_set_id}/rules/{rule_id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withRule(w, r, func(s *ruleSet, ru *rule) {
			delete(s.Rules, ru.ID)
			w.WriteHeader(http.StatusNoContent)
		})
	})
}

func (f *Server) withRuleSet(w http.ResponseWriter, r *http.Request, param string, fn func(*ruleSet)) {
	id, ok := pathID(r, param)
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.ruleSets[id]
	if !ok || s == nil {
		notFound(w)
		return
	}
	fn(s)
}

func (f *Server) withRule(w http.ResponseWriter, r *http.Request, fn func(*ruleSet, *rule)) {
	f.withRuleSet(w, r, "rules_set_id", func(s *ruleSet) {
		id, ok := pathID(r, "rule_id")
		ru := s.Rules[id]
		if !ok || ru == nil {
			notFound(w)
			return
		}
		fn(s, ru)
	})
}
