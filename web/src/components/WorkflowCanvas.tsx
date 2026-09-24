import { useState } from 'react';
import { applyNodeChanges, Background, Controls, ReactFlow } from '@xyflow/react';
import type { CanvasDraft, DraftNode, UpdateCanvasDraft } from '../canvasDraft';
import { makeCanvasNode, nodePalette, nodePosition } from '../canvasDraft';
import Badge from './Badge';
import CanvasNode from './CanvasNode';
import { fieldLabelClass, inputClass } from './classes';

// React Flow warns when this object changes between renders, so it lives at module scope.
const flowNodeTypes = { canvas: CanvasNode };

const barClass =
  'flex flex-wrap items-center justify-between gap-3 px-4 py-3.5 text-xs text-text-muted md:px-5 md:py-4';

const toolButtonClass =
  'rounded-[7px] border border-border-strong bg-surface px-3.75 py-2.5 text-sm whitespace-nowrap text-ink-strong hover:bg-surface-hover disabled:cursor-not-allowed';

type WorkflowCanvasProps = {
  draft: CanvasDraft;
  onChange: UpdateCanvasDraft;
  hasSavedWorkflow?: boolean;
};

export default function WorkflowCanvas({
  draft,
  onChange,
  hasSavedWorkflow = false,
}: WorkflowCanvasProps) {
  const [nodeKind, setNodeKind] = useState(nodePalette[0].kind);

  function addNode() {
    const type = nodePalette.find((item) => item.kind === nodeKind)!;
    const id = crypto.randomUUID();
    onChange((current) => {
      // Reuse a free grid position when earlier nodes have been removed.
      let index = 0;
      while (
        current.nodes.some(
          (node) =>
            node.position.x === nodePosition(index).x && node.position.y === nodePosition(index).y,
        )
      )
        index += 1;
      return {
        ...current,
        nodes: [...current.nodes, makeCanvasNode(type.kind, type.label, id, index)],
      };
    });
  }

  function resetLayout() {
    onChange((current) => ({
      ...current,
      nodes: current.nodes.map((node, index) => ({
        ...node,
        position: nodePosition(index),
        selected: false,
      })),
    }));
  }

  return (
    <section
      className="mb-7 overflow-hidden rounded-xl border border-border bg-surface"
      aria-label={`Canvas for ${draft.name || 'new workflow'}`}
    >
      <div className={`${barClass} border-b border-border-muted`}>
        <span className="min-w-0 wrap-anywhere">
          <strong className="mr-2.25 text-xs font-semibold text-ink-strong">
            {draft.name || 'Untitled workflow'}
          </strong>
          <Badge tone="neutral">Local draft</Badge>
        </span>
        <span className="min-w-0 wrap-anywhere">
          {draft.nodes.length} {draft.nodes.length === 1 ? 'node' : 'nodes'} · No connections
        </span>
      </div>
      <div className="flex flex-wrap items-end gap-3 border-b border-border-muted px-4 py-3.5 md:px-5 md:py-4">
        <label className={fieldLabelClass}>
          Node type
          <select
            className={inputClass}
            value={nodeKind}
            onChange={(event) => setNodeKind(event.target.value)}
          >
            {nodePalette.map((type) => (
              <option key={type.kind} value={type.kind}>
                {type.label}
              </option>
            ))}
          </select>
        </label>
        <button type="button" className={toolButtonClass} onClick={addNode}>
          Add node
        </button>
        <button
          type="button"
          className={`${toolButtonClass} ml-auto`}
          onClick={resetLayout}
          disabled={draft.nodes.length === 0}
        >
          Reset layout
        </button>
      </div>
      <div className="relative h-[450px] w-full md:h-[clamp(400px,62vh,760px)]">
        <ReactFlow<DraftNode>
          nodes={draft.nodes}
          edges={[]}
          nodeTypes={flowNodeTypes}
          onNodesChange={(changes) =>
            onChange((current) => ({
              ...current,
              nodes: applyNodeChanges(changes, current.nodes),
            }))
          }
          nodesConnectable={false}
          deleteKeyCode={['Backspace', 'Delete']}
          minZoom={0.3}
          maxZoom={2}
          fitView
          fitViewOptions={{ padding: 0.3, maxZoom: 1 }}
        >
          <Background gap={24} size={1.5} />
          <Controls showInteractive={false} />
        </ReactFlow>
        {draft.nodes.length === 0 && (
          <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2.5 p-6 text-center text-xs text-text-muted">
            <strong className="text-base text-ink-strong">Your workflow starts here</strong>
            <span>Choose a node type and add your first node.</span>
          </div>
        )}
      </div>
      <div className={`${barClass} border-t border-border-muted leading-[1.6]`}>
        <span>Drag to arrange · Select and press Delete to remove · Pan and zoom to explore</span>
        <span>
          {hasSavedWorkflow
            ? 'Canvas edits are local. Run workflow uses the saved steps below.'
            : 'Local draft only. Nodes do not run actions.'}
        </span>
      </div>
    </section>
  );
}
