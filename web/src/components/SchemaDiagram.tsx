import { memo, useMemo } from 'react'
import dagre from '@dagrejs/dagre'
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import { KeyMinimalisticIcon, LinkMinimalistic2Icon } from '@solar-icons/react/linear'
import type { Table } from '../api'
import { formatNumber } from '../format'
import { tableKey } from './schemaKey'

// Entity-relationship diagram: one card per table, one edge per foreign key,
// laid out left-to-right with dagre so related tables sit next to each other.

const NODE_WIDTH = 260
const HEADER = 44
const ROW = 26


type TableNodeData = { table: Table; selected: boolean; fkColumns: Set<string> }
type TableNodeType = Node<TableNodeData, 'table'>

const TableNode = memo(function TableNode({ data }: NodeProps<TableNodeType>) {
  const { table, selected, fkColumns } = data
  return (
    <div
      className={`overflow-hidden rounded-[8px] bg-surface-2 text-[12px] shadow-[0_6px_18px_-8px_rgb(0_0_0/0.55)] ${
        selected ? 'ring-2 ring-accent' : ''
      }`}
      style={{ width: NODE_WIDTH }}
    >
      <div className="flex h-11 items-center justify-between gap-2 bg-surface-3 px-3">
        <span className="truncate font-mono text-[12.5px] font-semibold">
          {table.schema !== 'public' && <span className="text-ink-3">{table.schema}.</span>}
          {table.name}
        </span>
        <span className="shrink-0 font-mono text-[11px] text-ink-2 tabular-nums">
          {table.kind === 'table' ? `${formatNumber(table.rows)} filas` : table.kind === 'view' ? 'vista' : 'vista mat.'}
        </span>
      </div>
      <ul>
        {table.columns.map((c) => (
          <li key={c.name} className="relative flex items-center gap-2 px-3 shadow-[inset_0_-1px_0_var(--hairline)] last:shadow-none" style={{ height: ROW }}>
            {/* Parents sit to the left of children: FK edges leave a child's
                column on its left edge and enter the parent's key on its right. */}
            <Handle type="source" position={Position.Left} id={`out-${c.name}`} className="!size-1.5 !border-0 !bg-transparent" />
            <span className="w-3.5 shrink-0 text-accent-text">
              {c.primaryKey ? <KeyMinimalisticIcon size={13} aria-label="clave primaria" /> : fkColumns.has(c.name) ? <LinkMinimalistic2Icon size={13} className="text-ink-2" aria-label="clave foránea" /> : null}
            </span>
            <span className={`truncate font-mono ${c.primaryKey ? 'font-semibold' : ''}`}>{c.name}</span>
            <span className="ml-auto truncate pl-2 font-mono text-[11px] text-ink-3">
              {c.type}
              {!c.nullable && !c.primaryKey ? ' *' : ''}
            </span>
            <Handle type="target" position={Position.Right} id={`in-${c.name}`} className="!size-1.5 !border-0 !bg-transparent" />
          </li>
        ))}
      </ul>
    </div>
  )
})

const nodeTypes = { table: TableNode }

function layout(tables: Table[], selected: string | null) {
  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', nodesep: 40, ranksep: 90, marginx: 20, marginy: 20 })
  g.setDefaultEdgeLabel(() => ({}))
  const known = new Set(tables.map(tableKey))
  for (const t of tables) {
    g.setNode(tableKey(t), { width: NODE_WIDTH, height: HEADER + Math.max(1, t.columns.length) * ROW })
  }
  const edges: Edge[] = []
  for (const t of tables) {
    for (const fk of t.foreignKeys) {
      const target = `${fk.refSchema}.${fk.refTable}`
      if (!known.has(target)) continue
      // Child (with the FK) on the right, parent on the left.
      g.setEdge(target, tableKey(t))
      edges.push({
        id: `${tableKey(t)}:${fk.name}`,
        source: tableKey(t),
        sourceHandle: `out-${fk.columns[0]}`,
        target,
        targetHandle: `in-${fk.refColumns[0]}`,
        type: 'smoothstep',
        label: fk.columns.length > 1 ? fk.columns.join(', ') : undefined,
        markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
        animated: selected !== null && (selected === tableKey(t) || selected === target),
      })
    }
  }
  dagre.layout(g)
  const nodes: TableNodeType[] = tables.map((t) => {
    const n = g.node(tableKey(t))
    return {
      id: tableKey(t),
      type: 'table',
      position: { x: n.x - n.width / 2, y: n.y - n.height / 2 },
      data: { table: t, selected: selected === tableKey(t), fkColumns: new Set(t.foreignKeys.flatMap((f) => f.columns)) },
    }
  })
  return { nodes, edges }
}

export default function SchemaDiagram({ tables, selected, onSelect }: { tables: Table[]; selected: string | null; onSelect: (key: string) => void }) {
  const { nodes, edges } = useMemo(() => layout(tables, selected), [tables, selected])
  return (
    <div className="h-[620px] w-full" role="img" aria-label={`Diagrama entidad-relación con ${tables.length} tablas. Usa la vista de árbol para navegar con teclado.`}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.15 }}
        minZoom={0.2}
        nodesConnectable={false}
        edgesFocusable={false}
        onNodeClick={(_, n) => onSelect(n.id)}
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="var(--surface-3)" />
        <Controls showInteractive={false} />
        {tables.length > 12 && <MiniMap pannable zoomable />}
      </ReactFlow>
    </div>
  )
}
