import { useState, useCallback, useMemo } from "react";
import { CopyButton } from "../CopyButton/CopyButton";

import "./JSONViewer.scss";

function collectPaths(value: unknown, path: string, out: Set<string>) {
  if (value === null || typeof value !== "object") return;
  const entries = Array.isArray(value)
    ? value.map((v, i) => [String(i), v] as const)
    : Object.entries(value);
  if (entries.length === 0) return;
  out.add(path);
  for (const [k, v] of entries) {
    collectPaths(v, `${path}.${k}`, out);
  }
}

interface JSONViewerProps {
  data: unknown;
  label?: string;
}

const JSONViewer = ({ data, label }: JSONViewerProps) => {
  const allPaths = useMemo(() => {
    const paths = new Set<string>();
    collectPaths(data, "$", paths);
    return paths;
  }, [data]);

  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(allPaths));

  const allExpanded = expanded.size >= allPaths.size;

  const handleToggle = () => {
    setExpanded(allExpanded ? new Set() : new Set(allPaths));
  };

  const toggleNode = useCallback((path: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }, []);

  return (
    <div className="json-viewer">
      <div className="json-viewer__header">
        {label && <h3 className="subtitle-m">{label}</h3>}
        <div className="json-viewer__actions">
          <button className="json-viewer__expand-all mono-xs" onClick={handleToggle}>
            {allExpanded ? "Collapse all" : "Expand all"}
          </button>
          <CopyButton value={JSON.stringify(data, null, 2)} />
        </div>
      </div>
      <div className="json-viewer__content mono-s">
        <JSONNode value={data} path="$" expanded={expanded} toggleNode={toggleNode} />
      </div>
    </div>
  );
};

interface JSONNodeProps {
  value: unknown;
  path: string;
  expanded: Set<string>;
  toggleNode: (path: string) => void;
}

const JSONNode = ({ value, path, expanded, toggleNode }: JSONNodeProps) => {
  if (value === null) {
    return <span className="json-viewer__null">null</span>;
  }

  if (typeof value === "boolean") {
    return (
      <span className="json-viewer__boolean">{value ? "true" : "false"}</span>
    );
  }

  if (typeof value === "number") {
    return <span className="json-viewer__number">{value}</span>;
  }

  if (typeof value === "string") {
    return <span className="json-viewer__string">"{value}"</span>;
  }

  if (Array.isArray(value)) {
    return (
      <CollapsibleNode
        kind="array"
        entries={value.map((item, i) => ({
          key: String(i),
          value: item,
          showKey: false,
        }))}
        count={value.length}
        path={path}
        expanded={expanded}
        toggleNode={toggleNode}
      />
    );
  }

  if (typeof value === "object") {
    const entries = Object.entries(value);
    return (
      <CollapsibleNode
        kind="object"
        entries={entries.map(([k, v]) => ({ key: k, value: v, showKey: true }))}
        count={entries.length}
        path={path}
        expanded={expanded}
        toggleNode={toggleNode}
      />
    );
  }

  return <span>{String(value)}</span>;
};

interface CollapsibleEntry {
  key: string;
  value: unknown;
  showKey: boolean;
}

interface CollapsibleNodeProps {
  kind: "object" | "array";
  entries: CollapsibleEntry[];
  count: number;
  path: string;
  expanded: Set<string>;
  toggleNode: (path: string) => void;
}

const CollapsibleNode = ({
  kind,
  entries,
  count,
  path,
  expanded,
  toggleNode,
}: CollapsibleNodeProps) => {
  const isExpanded = expanded.has(path);
  const toggle = useCallback(() => toggleNode(path), [toggleNode, path]);

  const openBracket = kind === "object" ? "{" : "[";
  const closeBracket = kind === "object" ? "}" : "]";

  if (count === 0) {
    return (
      <span className="json-viewer__bracket">
        {openBracket}{closeBracket}
      </span>
    );
  }

  const itemLabel = `${count} ${count === 1 ? "item" : "items"}`;
  const arrow = isExpanded ? "\u2191" : "\u2193";

  if (!isExpanded) {
    return (
      <button className="json-viewer__summary" onClick={toggle}>
        <span className="json-viewer__bracket">{openBracket}</span>
        {" "}
        <span className="json-viewer__summary-count">{itemLabel} {arrow}</span>
        {" "}
        <span className="json-viewer__bracket">{closeBracket}</span>
      </button>
    );
  }

  return (
    <span className="json-viewer__node">
      <button className="json-viewer__summary" onClick={toggle}>
        <span className="json-viewer__bracket">{openBracket}</span>
        {" "}
        <span className="json-viewer__summary-count">{itemLabel} {arrow}</span>
      </button>
      <div className="json-viewer__entries">
        {entries.map((entry, i) => (
          <div key={entry.key} className="json-viewer__entry">
            {entry.showKey && (
              <>
                <span className="json-viewer__key">"{entry.key}"</span>
                <span className="json-viewer__colon">: </span>
              </>
            )}
            <JSONNode
              value={entry.value}
              path={`${path}.${entry.key}`}
              expanded={expanded}
              toggleNode={toggleNode}
            />
            {i < entries.length - 1 && (
              <span className="json-viewer__comma">,</span>
            )}
          </div>
        ))}
      </div>
      <span className="json-viewer__bracket">{closeBracket}</span>
    </span>
  );
};

export default JSONViewer;
