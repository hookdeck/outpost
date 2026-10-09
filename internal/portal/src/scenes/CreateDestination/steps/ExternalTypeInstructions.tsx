import Markdown from "react-markdown";
import type { Components } from "react-markdown";
import Button from "../../../common/Button/Button";
import type { DestinationTypeReference } from "../../../typings/Destination";

// Links leave the portal in a new tab. react-markdown already escapes HTML and
// drops unsafe URLs such as javascript:.
const markdownComponents: Components = {
  a: ({ node: _node, ...props }) => (
    <a {...props} target="_blank" rel="noopener noreferrer" />
  ),
};

// ExternalTypeInstructions replaces the create form for a type whose
// create_mode is external, such as mcp: those destinations are created
// outside the portal, so there is nothing to submit.
export default function ExternalTypeInstructions({
  type,
}: {
  type: DestinationTypeReference;
}) {
  return (
    <>
      <div className="create-destination__step__header">
        <h1 className="title-xl">Connect {type.label}</h1>
        <p className="body-m muted">
          {type.label} destinations can't be created from this page. Follow the
          instructions below; the destination appears in your list once it is
          connected.
        </p>
      </div>
      <div className="create-destination__step__fields">
        <div className="external-instructions">
          {type.instructions ? (
            <Markdown components={markdownComponents}>
              {type.instructions}
            </Markdown>
          ) : (
            <p className="body-m muted">
              No setup instructions are available for this destination type.
            </p>
          )}
        </div>
      </div>
      <div className="create-destination__step__actions">
        <Button to="/">Back to destinations</Button>
      </div>
    </>
  );
}
