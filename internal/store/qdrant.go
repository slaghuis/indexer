package store

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
	"github.com/slaghuis/indexer/internal/chunker"
)

type Qdrant struct {
	client     *qdrant.Client
	collection string
	dim        uint64
}

func New(host string, port int, collection string, dim uint64) (*Qdrant, error) {
	c, err := qdrant.NewClient(&qdrant.Config{
		Host: host, Port: port,
	})
	if err != nil {
		return nil, err
	}
	q := &Qdrant{client: c, collection: collection, dim: dim}
	if err := q.ensureCollection(context.Background()); err != nil {
		return nil, err
	}
	return q, nil
}

func (q *Qdrant) ensureCollection(ctx context.Context) error {
	exists, err := q.client.CollectionExists(ctx, q.collection)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return q.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: q.collection,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     q.dim,
			Distance: qdrant.Distance_Cosine,
		}),
	})
}

func (q *Qdrant) Upsert(ctx context.Context, c chunker.Chunk, vec []float32) error {
	payload := qdrant.NewValueMap(map[string]any{
		"repo":       c.Repo,
		"path":       c.Path,
		"package":    c.Package,
		"symbol":     c.Symbol,
		"kind":       c.Kind,
		"signature":  c.Signature,
		"doc":        c.Doc,
		"body":       c.Body,
		"start_line": c.StartLine,
		"end_line":   c.EndLine,
		"hash":       c.Hash,
		"lang":       "go",
	})

	_, err := q.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: q.collection,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewIDUUID(c.StableID()),
				Vectors: qdrant.NewVectors(vec...),
				Payload: payload,
			},
		},
	})
	return err
}

// ExistingHash returns the hash of the point if it exists, "" otherwise.
func (q *Qdrant) ExistingHash(ctx context.Context, id string) (string, error) {
	resp, err := q.client.Get(ctx, &qdrant.GetPoints{
		CollectionName: q.collection,
		Ids:            []*qdrant.PointId{qdrant.NewIDUUID(id)},
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil || len(resp) == 0 {
		return "", err
	}
	if v, ok := resp[0].Payload["hash"]; ok {
		return v.GetStringValue(), nil
	}
	return "", nil
}

// DeleteByPath removes all chunks for a file (used when a file is deleted or renamed).
func (q *Qdrant) DeleteByPath(ctx context.Context, repo, path string) error {
	_, err := q.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: q.collection,
		Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{
			Must: []*qdrant.Condition{
				qdrant.NewMatch("repo", repo),
				qdrant.NewMatch("path", path),
			},
		}),
	})
	return err
}

// DeleteStale removes points for a file whose StableIDs are not in `keep`.
func (q *Qdrant) DeleteStale(ctx context.Context, repo, path string, keep map[string]bool) error {
	// Scroll all points for this file, filter in-memory, delete missing.
	var offset *qdrant.PointId
	var toDelete []*qdrant.PointId
	for {
		resp, err := q.client.Scroll(ctx, &qdrant.ScrollPoints{
			CollectionName: q.collection,
			Filter: &qdrant.Filter{
				Must: []*qdrant.Condition{
					qdrant.NewMatch("repo", repo),
					qdrant.NewMatch("path", path),
				},
			},
			Limit:       pointerU32(256),
			Offset:      offset,
			WithPayload: qdrant.NewWithPayload(false),
		})
		if err != nil {
			return err
		}
		for _, p := range resp {
			id := p.Id.GetUuid()
			if !keep[id] {
				toDelete = append(toDelete, p.Id)
			}
		}
		if len(resp) < 256 {
			break
		}
		offset = resp[len(resp)-1].Id
	}
	if len(toDelete) == 0 {
		return nil
	}
	_, err := q.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: q.collection,
		Points:         qdrant.NewPointsSelector(toDelete...),
	})
	if err != nil {
		return fmt.Errorf("delete stale: %w", err)
	}
	return nil
}

func pointerU32(v uint32) *uint32 { return &v }