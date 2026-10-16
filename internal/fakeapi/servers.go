// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type server struct {
	ID                int32
	Hostname          string
	Project           string
	Image             map[string]any
	Package           map[string]any
	Generation        string
	Status            string
	DestroyProtection bool
	HA                bool
	SSHPubKey         string
	Password          string
	UserData          string
	Power             string
	HasPublic         bool
	FwRulesSet        *string
	FwPolicyIn        string
	FwPolicyOut       string

	pollsLeft      int
	upgradeTo      map[string]any
	terminateAfter int
}

func (f *Server) serverDetail(s *server) map[string]any {
	d := f.serverSummary(s)
	d["ssh_pub_key"] = s.SSHPubKey
	d["username"] = s.Image["default_username"]
	d["volumes"] = f.serverVolumes(s.ID)
	d["floating_ips"] = f.serverFloatingIPs(s.ID)
	d["machine"] = map[string]any{"status": s.Power, "node": "fake-node-1"}
	return d
}

func (f *Server) serverSummary(s *server) map[string]any {
	ipv4, ipv6 := f.serverPrimaryIPs(s.ID)
	return map[string]any{
		"id": s.ID, "hostname": s.Hostname, "project": s.Project,
		"image": s.Image["name"], "package": s.Package["slug"],
		"cpus": s.Package["cpus"], "memory": s.Package["memory"], "disk_size": s.Package["disk_size"],
		"generation": s.Generation, "status": s.Status,
		"destroy_protection": s.DestroyProtection, "ha_enabled": s.HA, "custom_os": false,
		"rescue_mode": false, "boot_iso": nil, "rescue_supported": true,
		"networks": map[string]any{
			"public": map[string]any{
				"interface": "eth0", "ipv4": ipv4, "ipv6": ipv6,
				"interfaces": []any{map[string]any{"interface": "eth0", "ipv4": ipv4, "ipv6": ipv6, "primary": true}},
			},
			"private": f.serverPrivateInterfaces(s.ID),
		},
		// Present in the live API since after SDK v0.14.0; the SDK must
		// ignore it.
		"billing": nil,
	}
}

// applyFreeTierFirewall mirrors the live API: a free-tier server with no
// rule set of its own gets the platform's read-only set and an inbound DROP
// policy.
func (f *Server) applyFreeTierFirewall(s *server) {
	if s.Package["slug"] != freeTierPackage || !s.HasPublic || s.FwRulesSet != nil {
		return
	}
	id := strconv.Itoa(int(f.freeTierRuleSet().ID))
	s.FwRulesSet = &id
	s.FwPolicyIn = "DROP"
}

// advance moves asynchronous operations forward by one poll.
func (f *Server) advance(s *server) {
	switch s.Status {
	case "pending", "provisioning":
		s.pollsLeft--
		if s.pollsLeft <= 0 {
			if f.FailProvisioning {
				s.Status = "failed"
			} else {
				s.Status = "active"
				s.Power = "running"
			}
		} else {
			s.Status = "provisioning"
		}
	case "upgrading":
		s.pollsLeft--
		if s.pollsLeft <= 0 {
			s.Package = s.upgradeTo
			s.Status = "active"
		}
	case "terminating":
		s.terminateAfter--
		if s.terminateAfter <= 0 {
			f.removeServer(s.ID)
		}
	}
}

func (f *Server) findCatalog(list []map[string]any, ref string) map[string]any {
	for _, e := range list {
		if fmt.Sprint(e["id"]) == ref || e["slug"] == ref {
			return e
		}
	}
	return nil
}

// ServerIDs returns the IDs of all servers.
func (f *Server) ServerIDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.servers)
}

// ServerByHostname returns the ID of the server with this hostname, or 0.
func (f *Server) ServerByHostname(hostname string) int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.servers {
		if s.Hostname == hostname {
			return id
		}
	}
	return 0
}

// ServerUserData returns the user data a server was created with.
func (f *Server) ServerUserData(id int32) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.servers[id].UserData
}

// SetServerProject changes a server's project out of band.
func (f *Server) SetServerProject(id int32, project string) {
	f.Do(func() { f.servers[id].Project = project })
}

// SetServerPower changes a server's power state out of band.
func (f *Server) SetServerPower(id int32, power string) {
	f.Do(func() { f.servers[id].Power = power })
}

// SetServerPackage changes a server's package out of band.
func (f *Server) SetServerPackage(id int32, slug string) {
	f.Do(func() { f.servers[id].Package = f.findCatalog(f.packages, slug) })
}

// DetachServerFirewall clears a server's rule set out of band.
func (f *Server) DetachServerFirewall(id int32) {
	f.Do(func() { f.servers[id].FwRulesSet = nil })
}

// DeleteServer removes a server out of band.
func (f *Server) DeleteServer(id int32) {
	f.Do(func() { f.removeServer(id) })
}

func (f *Server) removeServer(id int32) {
	delete(f.servers, id)
	f.onServerRemoved(id)
}

func (f *Server) ruleSetInUse(setID int32) bool {
	want := strconv.Itoa(int(setID))
	for _, s := range f.servers {
		if s.FwRulesSet != nil && *s.FwRulesSet == want {
			return true
		}
	}
	return false
}

func (f *Server) hostnameOf(id int32) string {
	if s := f.servers[id]; s != nil {
		return s.Hostname
	}
	return ""
}

func (f *Server) serverRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cloud/servers/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []any{}
		for _, id := range sortedKeys(f.servers) {
			items = append(items, f.serverSummary(f.servers[id]))
		}
		writeJSON(w, http.StatusOK, paginate(r, items))
	})

	mux.HandleFunc("POST /api/cloud/servers/{$}", func(w http.ResponseWriter, r *http.Request) {
		b := bodyOf(r)
		f.mu.Lock()
		defer f.mu.Unlock()
		imgRef, _ := str(b, "image")
		img := f.findCatalog(f.images, imgRef)
		if img == nil {
			badRequest(w, "image", "Image not found.")
			return
		}
		pkgRef, _ := str(b, "package")
		pkg := f.findCatalog(f.packages, pkgRef)
		if pkg == nil {
			badRequest(w, "package", "Package not found.")
			return
		}
		s := &server{
			ID: f.id(), Image: img, Package: pkg, Generation: "gen2", Status: "pending",
			Power: "stopped", FwPolicyIn: "ACCEPT", FwPolicyOut: "ACCEPT", pollsLeft: f.PollsUntilActive,
		}
		s.Hostname, _ = str(b, "hostname")
		if s.Hostname == "" {
			s.Hostname = fmt.Sprintf("srv-%d", s.ID)
		}
		s.Project, _ = str(b, "project")
		s.Password, _ = str(b, "password")
		s.SSHPubKey, _ = str(b, "ssh_pub_key")
		s.UserData, _ = str(b, "user_data")
		if g, ok := str(b, "generation"); ok {
			s.Generation = g
		}
		s.HA, _ = boolean(b, "enable_ha")
		if keyRef, ok := str(b, "ssh_pub_key_id"); ok {
			found := false
			for _, k := range f.sshKeys {
				if strconv.Itoa(int(k.ID)) == keyRef || k.json()["fingerprint"] == keyRef {
					s.SSHPubKey, found = k.Key, true
				}
			}
			if !found {
				badRequest(w, "ssh_pub_key_id", "SSH key not found.")
				return
			}
		}
		if v, ok := str(b, "fw_rules_set"); ok {
			id, _ := strconv.Atoi(v)
			if f.ruleSets[int32(id)] == nil {
				badRequest(w, "fw_rules_set", "Firewall rules set not found.")
				return
			}
			s.FwRulesSet = &v
		}
		if v, ok := str(b, "fw_policy_in"); ok {
			s.FwPolicyIn = v
		}
		if v, ok := str(b, "fw_policy_out"); ok {
			s.FwPolicyOut = v
		}
		if field, msg := f.attachAtCreate(s, b); field != "" {
			badRequest(w, field, msg)
			return
		}
		f.applyFreeTierFirewall(s)
		f.servers[s.ID] = s
		writeJSON(w, http.StatusCreated, map[string]any{"id": s.ID})
	})

	mux.HandleFunc("GET /api/cloud/servers/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			f.advance(s)
			if f.servers[s.ID] == nil {
				notFound(w)
				return
			}
			writeJSON(w, http.StatusOK, f.serverDetail(s))
		})
	})

	mux.HandleFunc("PATCH /api/cloud/servers/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			b := bodyOf(r)
			if v, ok := str(b, "project"); ok {
				s.Project = v
			}
			if v, ok := str(b, "password"); ok {
				s.Password = v
			}
			if v, ok := str(b, "ssh_pub_key"); ok {
				s.SSHPubKey = v
			}
			writeJSON(w, http.StatusOK, f.serverDetail(s))
		})
	})

	mux.HandleFunc("DELETE /api/cloud/servers/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			if s.DestroyProtection {
				writeJSON(w, http.StatusForbidden, map[string]any{"detail": "Destroy protection is enabled."})
				return
			}
			s.Status = "terminating"
			s.terminateAfter = 2
			w.WriteHeader(http.StatusNoContent)
		})
	})

	mux.HandleFunc("POST /api/cloud/servers/{id}/modify-package/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			if s.Status != "active" {
				badRequest(w, "detail", "The server is busy.")
				return
			}
			ref, _ := str(bodyOf(r), "package")
			pkg := f.findCatalog(f.packages, ref)
			if pkg == nil {
				badRequest(w, "package", "Package not found.")
				return
			}
			s.Status, s.upgradeTo, s.pollsLeft = "upgrading", pkg, f.PollsUntilActive
			writeJSON(w, http.StatusOK, map[string]any{"upgrading": true})
		})
	})

	mux.HandleFunc("GET /api/cloud/servers/{id}/power-management/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			writeJSON(w, http.StatusOK, map[string]any{"status": s.Power})
		})
	})
	mux.HandleFunc("POST /api/cloud/servers/{id}/power-management/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			switch action, _ := str(bodyOf(r), "action"); action {
			case "start", "reboot":
				s.Power = "running"
			case "stop", "shutdown":
				s.Power = "stopped"
			default:
				badRequest(w, "action", `"`+action+`" is not a valid choice.`)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": s.Power})
		})
	})

	mux.HandleFunc("POST /api/cloud/servers/{id}/destroy-protection/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			v, ok := boolean(bodyOf(r), "destroy_protection")
			if !ok {
				badRequest(w, "destroy_protection", "This field is required.")
				return
			}
			s.DestroyProtection = v
			writeJSON(w, http.StatusOK, map[string]any{"destroy_protection": v})
		})
	})

	publicInterface := func(s *server) map[string]any {
		ipv4, ipv6 := f.serverPrimaryIPs(s.ID)
		var set any
		if s.FwRulesSet != nil {
			set = *s.FwRulesSet
			// The live API reports the free-tier set by its name in lower
			// case, not by ID; ReportRuleSetsByName extends that to every
			// set.
			n, _ := strconv.Atoi(*s.FwRulesSet)
			if rs := f.ruleSets[int32(n)]; rs != nil && (rs.ReadOnly || f.ReportRuleSetsByName) {
				set = strings.ToLower(rs.Name)
			}
		}
		return map[string]any{
			"interface": "eth0", "ipv4": ipv4, "ipv6": ipv6,
			"fw_rules_set": set, "fw_policy_in": s.FwPolicyIn, "fw_policy_out": s.FwPolicyOut,
		}
	}
	mux.HandleFunc("GET /api/cloud/servers/{id}/public-interface/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			if !s.HasPublic {
				notFound(w)
				return
			}
			writeJSON(w, http.StatusOK, publicInterface(s))
		})
	})
	mux.HandleFunc("POST /api/cloud/servers/{id}/public-interface/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.withServer(w, r, func(s *server) {
			if !s.HasPublic {
				notFound(w)
				return
			}
			b := bodyOf(r)
			// Mirrors the API: an absent fw_rules_set leaves the set
			// unchanged; only an explicit null detaches it.
			if v, present := b["fw_rules_set"]; present {
				if v == nil {
					s.FwRulesSet = nil
				} else {
					id := fmt.Sprint(v)
					n, _ := strconv.Atoi(id)
					if f.ruleSets[int32(n)] == nil {
						badRequest(w, "fw_rules_set", "Firewall rules set not found.")
						return
					}
					s.FwRulesSet = &id
				}
			}
			if v, ok := str(b, "fw_policy_in"); ok {
				s.FwPolicyIn = v
			}
			if v, ok := str(b, "fw_policy_out"); ok {
				s.FwPolicyOut = v
			}
			writeJSON(w, http.StatusOK, publicInterface(s))
		})
	})
}

func (f *Server) withServer(w http.ResponseWriter, r *http.Request, fn func(*server)) {
	id, ok := pathID(r, "id")
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.servers[id]
	if !ok || s == nil {
		notFound(w)
		return
	}
	fn(s)
}

// SetServerHostname changes how the API reports a server's hostname.
func (f *Server) SetServerHostname(id int32, hostname string) {
	f.Do(func() { f.servers[id].Hostname = hostname })
}

// CreateServer creates an active server directly in the fake, as if it had
// been created outside Terraform. It returns the server ID.
func (f *Server) CreateServer(hostname, imageSlug, packageSlug string) int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := &server{
		ID: f.id(), Hostname: hostname, Image: f.findCatalog(f.images, imageSlug),
		Package: f.findCatalog(f.packages, packageSlug), Generation: "gen2", Status: "active",
		Power: "running", FwPolicyIn: "ACCEPT", FwPolicyOut: "ACCEPT", HasPublic: true,
	}
	f.allocateIP(false).ServerID = s.ID
	f.applyFreeTierFirewall(s)
	f.servers[s.ID] = s
	return s.ID
}
