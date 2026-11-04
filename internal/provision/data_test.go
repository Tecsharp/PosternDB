package provision

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/testsupport"
)

func TestDataEditor(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.insp.Data = model.TableData{Columns: []model.DataColumn{{Name: "id", PrimaryKey: true}}, Editable: true}
	ctx := context.Background()
	v := "x"

	d, err := h.svc.Browse(ctx, "acme", BrowseRequest{Schema: "public", Table: "t", Page: 2, Size: 10, Sort: "id", Desc: true})
	require.NoError(t, err)
	assert.True(t, d.Editable)
	assert.Equal(t, "browse public.t page=2 size=10 sort=id desc=true total=count", h.insp.Queries[0])

	known := int64(1234)
	_, err = h.svc.Browse(ctx, "acme", BrowseRequest{Schema: "public", Table: "t", Page: 3, Size: 10, KnownTotal: &known})
	require.NoError(t, err)
	assert.Equal(t, "browse public.t page=3 size=10 sort= desc=false total=1234", h.insp.Queries[1], "a total the caller already has is not counted again")

	require.NoError(t, h.svc.InsertRow(ctx, "acme", "public", "t", model.RowValues{"name": &v}, "admin"))
	require.NoError(t, h.svc.UpdateRow(ctx, "acme", "public", "t", model.RowValues{"id": &v}, model.RowValues{"name": &v}, "admin"))
	require.NoError(t, h.svc.DeleteRow(ctx, "acme", "public", "t", model.RowValues{"id": &v}, "admin"))
	assert.Len(t, h.insp.Writes, 3)
	actions := h.meta.Actions()
	assert.Equal(t, []string{config.ActionRowInserted, config.ActionRowUpdated, config.ActionRowDeleted}, actions[len(actions)-3:])
	assert.Equal(t, "public.t", h.meta.Audit[len(h.meta.Audit)-1].Details["table"])

	h.insp.FailWrite = true
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, h.svc.InsertRow(ctx, "acme", "public", "t", nil, "admin"), &invalidErr, "database errors are shown to the operator")
	h.insp.FailRead = true
	_, err = h.svc.Browse(ctx, "acme", BrowseRequest{})
	assert.ErrorAs(t, err, &invalidErr)

	_, err = h.svc.Browse(ctx, "ghost", BrowseRequest{})
	assert.ErrorIs(t, err, model.ErrNotFound)
	assert.ErrorIs(t, h.svc.DeleteRow(ctx, "ghost", "s", "t", nil, "a"), model.ErrNotFound)
}
