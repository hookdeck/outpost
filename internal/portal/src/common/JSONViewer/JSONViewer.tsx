import { useState, useCallback } from "react";
import { CopyButton } from "../CopyButton/CopyButton";

import "./JSONViewer.scss";

interface JSONViewerProps {
  data: unknown;
  label?: string;
}

const JSONViewer = ({ data, label }: JSONViewerProps) => {
  const [expandAllKey, setExpandAllKey] = useState(0);
  const [isExpanded, setIsExpanded] = useState(true);

  const handleExpandAll = () => {
    setIsExpanded(true);
    setExpandAllKey((k) => k + 1);
  };

  const handleCollapseAll = () => {
    setIsExpanded(false);
    setExpandAllKey((k) => k + 1);
  };

  return (
    <div className="json-viewer">
      <div className="json-viewer__header">
        {label && <h3 className="subtitle-m">{label}</h3>}
        <div className="json-viewer__actions">
          <button
            className="json-viewer__expand-all mono-xs"
            onClick={isExpanded ? handleCollapseAll : handleExpandAll}
          >
            {isExpanded ? "Collapse all" : "Expand all"}
          </button>
          <CopyButton value={JSON.stringify(data, null, 2)} />
        </div>
      </div>
      <div className="json-viewer__content mono-s">
        <JSONNode key={expandAllKey} value={data} depth={0} defaultExpanded={isExpanded} />
      </div>
    </div>
  );
};

interface JSONNodeProps {
  value: unknown;
  depth: number;
  defaultExpanded?: boolean;
}

const JSONNode = ({ value, depth, defaultExpanded = false }: JSONNodeProps) => {
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
        depth={depth}
        defaultExpanded={defaultExpanded}
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
        depth={depth}
        defaultExpanded={defaultExpanded}
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
  depth: number;
  defaultExpanded: boolean;
}

const CollapsibleNode = ({
  kind,
  entries,
  count,
  depth,
  defaultExpanded,
}: CollapsibleNodeProps) => {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const toggle = useCallback(() => setExpanded((e) => !e), []);

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
  const arrow = expanded ? "\u2191" : "\u2193";

  if (!expanded) {
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
            <JSONNode value={entry.value} depth={depth + 1} defaultExpanded={defaultExpanded} />
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
