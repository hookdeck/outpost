import { Link } from "react-router-dom";
import "./Table.scss";

interface Column {
  header: string;
  width?: number;
}

interface TableRow {
  id: string;
  entries: (string | React.ReactNode)[];
  link?: string;
  onClick?: () => void;
  active?: boolean;
}

interface TableProps {
  columns: Column[];
  rows: TableRow[];
  footer_label?: string;
  footer?: React.ReactNode;
}

const Table: React.FC<TableProps> = ({
  columns,
  rows,
  footer,
  footer_label,
}) => {
  const handle_row_click = (row: TableRow) => {
    if (row.onClick) {
      row.onClick();
    }
  };

  const columns_widths = columns
    .map((column) => (column.width ? `${column.width}px` : "1fr"))
    .join(" ");

  return (
    <div className="table">
      <div
        className="table__header"
        style={{
          gridTemplateColumns: columns_widths,
        }}
      >
        {columns.map((column, index) => (
          <div
            key={`header-${index}`}
            className="table__header-cell subtitle-xs"
            style={{
              maxWidth: column.width,
              minWidth: column.width,
            }}
          >
            {column.header}
          </div>
        ))}
      </div>
      <div className="table__body">
        {rows.map((row) => (
          <div
            className="table__body-row"
            key={row.id}
            style={{
              gridTemplateColumns: columns_widths,
            }}
            onClick={() => handle_row_click(row)}
            data-active={row.active}
          >
            {row.entries.map((entry, index) => (
              <div
                className="table__body-cell"
                key={`${row.id}-${index}`}
                style={{
                  maxWidth: columns[index]?.width,
                  minWidth: columns[index]?.width,
                }}
              >
                {row.link ? (
                  <Link to={row.link}>
                    {typeof entry === "string" ? <span>{entry}</span> : entry}
                  </Link>
                ) : (
                  entry
                )}
              </div>
            ))}
          </div>
        ))}
      </div>
      {footer ?? (
        <div className="table__footer">
          <div>
            <span className="subtitle-s">{rows.length}</span>
            <span className="body-s"> {footer_label}</span>
          </div>
        </div>
      )}
    </div>
  );
};

export default Table;
