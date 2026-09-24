import { Handle, Position } from '@xyflow/react';
import type { NodeProps } from '@xyflow/react';
import { nodeKindClass } from '../canvasDraft';
import type { DraftNode } from '../canvasDraft';

const handleClass = 'size-2 border-2 border-white bg-node-handle';

export default function CanvasNode({ data, selected }: NodeProps<DraftNode>) {
  return (
    <div
      className={`w-[200px] rounded-[10px] border border-l-4 border-border-strong bg-surface px-5 py-6 text-center text-sm leading-[normal] font-semibold wrap-anywhere text-ink-strong shadow-node ${
        nodeKindClass[data.kind] ?? nodeKindClass['http.check']
      } ${selected ? 'outline-2 outline-offset-[3px] outline-node-http' : ''}`}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} className={handleClass} />
      {data.label}
      <Handle
        type="source"
        position={Position.Bottom}
        isConnectable={false}
        className={handleClass}
      />
    </div>
  );
}
