package provision

import (
	"context"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

// BrowseRequest selects a page of a table.
type BrowseRequest struct {
	Schema, Table string
	Page, Size    int
	Sort          string
	Desc          bool
}

// Browse returns one page of rows of a table.
func (s *Service) Browse(ctx context.Context, slug string, req BrowseRequest) (model.TableData, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.TableData{}, err
	}
	data, err := s.inspector.Browse(ctx, db.DBName, db.RoleName, req.Schema, req.Table, req.Page, req.Size, req.Sort, req.Desc)
	if err != nil {
		return data, &InvalidInputError{Err: err}
	}
	return data, nil
}

// InsertRow adds a row; every change to client data is audited.
func (s *Service) InsertRow(ctx context.Context, slug, schema, table string, values model.RowValues, actor string) error {
	return s.writeRow(ctx, slug, schema, table, config.ActionRowInserted, nil, actor, func(db model.ClientDatabase) error {
		return s.inspector.InsertRow(ctx, db.DBName, db.RoleName, schema, table, values)
	})
}

// UpdateRow changes columns of one row identified by its primary key.
func (s *Service) UpdateRow(ctx context.Context, slug, schema, table string, key, values model.RowValues, actor string) error {
	return s.writeRow(ctx, slug, schema, table, config.ActionRowUpdated, key, actor, func(db model.ClientDatabase) error {
		return s.inspector.UpdateRow(ctx, db.DBName, db.RoleName, schema, table, key, values)
	})
}

// DeleteRow removes one row identified by its primary key.
func (s *Service) DeleteRow(ctx context.Context, slug, schema, table string, key model.RowValues, actor string) error {
	return s.writeRow(ctx, slug, schema, table, config.ActionRowDeleted, key, actor, func(db model.ClientDatabase) error {
		return s.inspector.DeleteRow(ctx, db.DBName, db.RoleName, schema, table, key)
	})
}

func (s *Service) writeRow(ctx context.Context, slug, schema, table, action string, key model.RowValues, actor string, fn func(model.ClientDatabase) error) error {
	db, err := s.active(ctx, slug)
	if err != nil {
		return err
	}
	if err := fn(db); err != nil {
		return &InvalidInputError{Err: err}
	}
	s.recordAudit(ctx, actor, action, db.Slug, map[string]any{"table": schema + "." + table, "key": key})
	return nil
}
