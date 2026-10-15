// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package fakeapi

import (
	"net/http"
	"strconv"
)

type volume struct {
	ID       int32
	Project  string
	Alias    string
	Size     int
	Product  string
	ServerID int32
}

func (v *volume) json(f *Server) map[string]any {
	return map[string]any{
		"id": v.ID, "project": v.Project, "alias": v.Alias, "size": v.Size, "product": v.Product,
		"attached": v.ServerID != 0, "server": f.hostnameOf(v.ServerID), "billing": nil,
	}
}

// VolumeIDs returns the IDs of all volumes.
func (f *Server) VolumeIDs() []int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sortedKeys(f.volumes)
}

// DetachVolume detaches a volume out of band.
func (f *Server) DetachVolume(id int32) {
	f.Do(func() { f.volumes[id].ServerID = 0 })
}

// VolumeServer returns the server a volume is attached to, or 0.
func (f *Server) VolumeServer(id int32) int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.volumes[id].ServerID
}

// CreateDetachedVolume creates a volume outside Terraform and detaches it.
func (f *Server) CreateDetachedVolume(alias string, size int) int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := &volume{ID: f.id(), Alias: alias, Size: size, Product: "ssd"}
	f.volumes[v.ID] = v
	return v.ID
}

func (f *Server) createVolume(serverID int32, product string, size int, alias, project string) *volume {
	v := &volume{ID: f.id(), Project: project, Alias: alias, Size: size, Product: product, ServerID: serverID}
	f.volumes[v.ID] = v
	return v
}

func (f *Server) serverVolumes(sid int32) []any {
	out := []any{}
	for _, id := range sortedKeys(f.volumes) {
		if v := f.volumes[id]; v.ServerID == sid {
			out = append(out, v.json(f))
		}
	}
	return out
}

func (f *Server) volumeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/cloud/servers/{server_id}/volumes/{$}", func(w http.ResponseWriter, r *http.Request) {
		sid, _ := strconv.Atoi(r.PathValue("server_id"))
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.servers[int32(sid)] == nil {
			notFound(w)
			return
		}
		b := bodyOf(r)
		product, _ := str(b, "product")
		if f.findCatalog(f.storageProducts, product) == nil {
			badRequest(w, "product", "Storage product not found.")
			return
		}
		size, _ := integer(b, "size")
		if size < 10 {
			badRequest(w, "size", "Ensure this value is greater than or equal to 10.")
			return
		}
		alias, _ := str(b, "alias")
		project, _ := str(b, "project")
		writeJSON(w, http.StatusCreated, f.createVolume(int32(sid), product, size, alias, project).json(f))
	})
	mux.HandleFunc("GET /api/cloud/volumes/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		items := []any{}
		for _, id := range sortedKeys(f.volumes) {
			items = append(items, f.volumes[id].json(f))
		}
		writeJSON(w, http.StatusOK, items)
	})
	with := func(w http.ResponseWriter, r *http.Request, fn func(v *volume)) {
		id, ok := pathID(r, "id")
		f.mu.Lock()
		defer f.mu.Unlock()
		v := f.volumes[id]
		if !ok || v == nil {
			notFound(w)
			return
		}
		fn(v)
	}
	mux.HandleFunc("GET /api/cloud/volumes/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(v *volume) { writeJSON(w, http.StatusOK, v.json(f)) })
	})
	mux.HandleFunc("PATCH /api/cloud/volumes/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(v *volume) {
			b := bodyOf(r)
			if size, ok := integer(b, "size"); ok {
				if size < v.Size {
					badRequest(w, "size", "Volumes cannot be shrunk.")
					return
				}
				v.Size = size
			}
			if a, ok := str(b, "alias"); ok {
				v.Alias = a
			}
			if p, ok := str(b, "project"); ok {
				v.Project = p
			}
			writeJSON(w, http.StatusOK, v.json(f))
		})
	})
	mux.HandleFunc("DELETE /api/cloud/volumes/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(v *volume) {
			if v.ServerID != 0 {
				badRequest(w, "detail", "Detach the volume before deleting it.")
				return
			}
			delete(f.volumes, v.ID)
			w.WriteHeader(http.StatusNoContent)
		})
	})
	mux.HandleFunc("POST /api/cloud/volumes/{id}/attach/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(v *volume) {
			vm, _ := integer(bodyOf(r), "vm")
			if f.servers[int32(vm)] == nil {
				badRequest(w, "vm", "Server not found.")
				return
			}
			if v.ServerID != 0 {
				badRequest(w, "detail", "The volume is already attached.")
				return
			}
			v.ServerID = int32(vm)
			writeJSON(w, http.StatusOK, map[string]any{"attached": true})
		})
	})
	mux.HandleFunc("POST /api/cloud/volumes/{id}/detach/{$}", func(w http.ResponseWriter, r *http.Request) {
		with(w, r, func(v *volume) {
			was := v.ServerID != 0
			v.ServerID = 0
			writeJSON(w, http.StatusOK, map[string]any{"detached": was})
		})
	})
}

// CreateVolumeOn creates a volume attached to a server, outside Terraform.
// product is stored verbatim, as the API would report it.
func (f *Server) CreateVolumeOn(serverID int32, product string, size int, alias string) int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createVolume(serverID, product, size, alias, "").ID
}
