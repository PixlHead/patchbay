import { useState } from 'react';
import { applyNodeChanges, Background, Controls, ReactFlow } from '@xyflow/react';
import type { CanvasDraft, CanvasNode, UpdateCanvasDraft } from '../canvasDraft';
import { makeCanvasNode, nodePosition, nodeTypes } from '../canvasDraft';

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
  const [nodeKind, setNodeKind] = useState(nodeTypes[0].kind);

  function addNode() {
    const type = nodeTypes.find((item) => item.kind === nodeKind)!;
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
      className="canvas-panel workflow-canvas"
      aria-label={`Canvas for ${draft.name || 'new workflow'}`}
    >
      <div className="canvas-toolbar">
        <span>
          <strong>{draft.name || 'Untitled workflow'}</strong>
          <span className="badge">Local draft</span>
        </span>
        <span>
          {draft.nodes.length} {draft.nodes.length === 1 ? 'node' : 'nodes'} · No connections
        </span>
      </div>
      <div className="canvas-actions">
        <label className="canvas-node-picker">
          Node type
          <select value={nodeKind} onChange={(event) => setNodeKind(event.target.value)}>
            {nodeTypes.map((type) => (
              <option key={type.kind} value={type.kind}>
                {type.label}
              </option>
            ))}
          </select>
        </label>
        <button type="button" className="canvas-reset" onClick={addNode}>
          Add node
        </button>
        <button
          type="button"
          className="canvas-reset canvas-layout-button"
          onClick={resetLayout}
          disabled={draft.nodes.length === 0}
        >
          Reset layout
        </button>
      </div>
      <div className="flow-canvas">
        <ReactFlow<CanvasNode>
          nodes={draft.nodes}
          edges={[]}
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
          <Background color="#ccd8d0" gap={24} size={1.5} />
          <Controls showInteractive={false} />
        </ReactFlow>
        {draft.nodes.length === 0 && (
          <div className="canvas-empty">
            <strong>Your workflow starts here</strong>
            <span>Choose a node type and add your first node.</span>
          </div>
        )}
      </div>
      <div className="canvas-caption">
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
