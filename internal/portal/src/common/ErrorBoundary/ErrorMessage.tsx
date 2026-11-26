import "./ErrorBoundary.scss";

export default ({ code }: { code?: string }) => (
  <div className="error-boundary">
    <h1 className="title-l">Something went wrong.</h1>
    <p className="body-s muted">Please try refreshing the page.</p>
    {code && <p className="body-s muted">Error code: {code}</p>}
  </div>
);
