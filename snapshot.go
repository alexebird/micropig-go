package main

import (
	"errors"
	"strconv"

	"github.com/digitalocean/godo"
)

func (m *Micropig) GetSnapshotIdBySlug(slug string) (int, error) {
	listOpts := &godo.ListOptions{Page: 1, PerPage: 200}

	snapshots, _, err := m.DoClient.Snapshots.ListDroplet(m.Ctx, listOpts)
	if err != nil {
		return -1, err
	}

	if len(snapshots) <= 0 {
		return -1, errors.New("no snapshots found")
	}

	var snapshot godo.Snapshot

	for _, snap := range snapshots {
		if snap.Name == slug {
			snapshot = snap
			break
		}
	}

	snapID, err := strconv.ParseInt(snapshot.ID, 0, 64)
	if err != nil {
		return -1, err
	}

	return int(snapID), nil
}
