import type { Node } from '@xyflow/react';
import type { Workflow } from './api';

export type CanvasNode = Node<{ label: string; kind: string }>;

export type CanvasDraft = {
  name: string;
  nodes: CanvasNode[];
};

export type LocalWorkflow = {
  local: true;
  id: string;
  name: string;
  description: string;
};

export type CanvasDrafts = Record<string, CanvasDraft>;
export type UpdateCanvasDraft = (update: (draft: CanvasDraft) => CanvasDraft) => void;

export const nodeTypes = [
  { kind: 'http.check', label: 'HTTP check', className: 'canvas-node-http' },
  { kind: 'ssh.command', label: 'SSH command', className: 'canvas-node-ssh' },
  { kind: 'discord.alert', label: 'Discord alert', className: 'canvas-node-discord' },
];

export function nodePosition(index: number) {
  return { x: (index % 3) * 280, y: Math.floor(index / 3) * 160 };
}

export function makeCanvasNode(kind: string, label: string, id: string, index: number): CanvasNode {
  const type = nodeTypes.find((item) => item.kind === kind);
  return {
    id,
    position: nodePosition(index),
    data: { label, kind },
    className: `canvas-node ${type?.className ?? ''}`,
  };
}

export function draftFromWorkflow(workflow: Workflow | LocalWorkflow): CanvasDraft {
  return {
    name: workflow.name,
    nodes:
      'steps' in workflow
        ? workflow.steps.map((step, index) => makeCanvasNode(step.type, step.name, step.id, index))
        : [],
  };
}
