import { Link } from "react-router-dom";
import "./NotFoundState.scss";

interface NotFoundStateProps {
  title: string;
  message: React.ReactNode;
  backTo?: string;
  backLabel?: string;
}

// NotFoundState is the page body shown in place of something the portal
// can't display: an unknown route, a missing or hidden destination, or a
// destination type the portal doesn't know.
const NotFoundState: React.FC<NotFoundStateProps> = ({
  title,
  message,
  backTo = "/",
  backLabel = "Back to Destinations",
}) => (
  <div className="not-found-state">
    <h1 className="not-found-state__title">{title}</h1>
    <p className="not-found-state__message">{message}</p>
    <Link to={backTo} className="not-found-state__link">
      ← {backLabel}
    </Link>
  </div>
);

export default NotFoundState;
