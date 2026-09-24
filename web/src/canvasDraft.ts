import type { Node } from '@xyflow/react';
import type { Workflow } from './api';

export type DraftNode = Node<{ label: string; kind: string }, 'canvas'>;

export type CanvasDraft = {
  name: string;
  nodes: DraftNode[];
};

export type LocalWorkflow = {
  local: true;
  id: string;
  name: string;
  description: string;
};

export type CanvasDrafts = Record<string, CanvasDraft>;
export type UpdateCanvasDraft = (update: (draft: CanvasDraft) => CanvasDraft) => void;

// Kinds the picker offers. Saved steps can carry other kinds, such as tcp.check.
export const nodePalette = [
  { kind: 'http.check', label: 'HTTP check' },
  { kind: 'ssh.command', label: 'SSH command' },
  { kind: 'discord.alert', label: 'Discord alert' },
];

// Left accent per node kind. Every value is a complete class so Tailwind can find it.
export const nodeKindClass: Record<string, string> = {
  'http.check': 'border-l-node-http',
  'tcp.check': 'border-l-node-http',
  'ssh.command': 'border-l-node-ssh',
  'discord.alert': 'border-l-node-discord',
};

export function nodePosition(index: number) {
  return { x: (index % 3) * 280, y: Math.floor(index / 3) * 160 };
}

export function makeCanvasNode(kind: string, label: string, id: string, index: number): DraftNode {
  return { id, type: 'canvas', position: nodePosition(index), data: { label, kind } };
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
