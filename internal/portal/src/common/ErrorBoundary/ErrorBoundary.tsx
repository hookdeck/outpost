import { useEffect, useRef } from "react";
import Sentry from "./../../Sentry";

import ErrorMessage from "./ErrorMessage";
import { useLocation } from "react-router-dom";

const Fallback = ({ resetError }: { resetError: () => void }) => {
  const { pathname } = useLocation();
  const errorPathname = useRef(pathname);

  // Reset only after navigation: resetting on mount re-renders the page that just threw, which loops.
  useEffect(() => {
    if (pathname !== errorPathname.current) {
      resetError();
    }
  }, [pathname]);

  return <ErrorMessage />;
};

export default ({ children }: { children: React.ReactNode }) => (
  <Sentry.ErrorBoundary fallback={Fallback}>{children}</Sentry.ErrorBoundary>
);
