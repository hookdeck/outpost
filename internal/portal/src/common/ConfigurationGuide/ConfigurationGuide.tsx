import type { DestinationTypeReference } from "../../typings/Destination";
import Markdown from "react-markdown";

export const ConfigurationGuide = ({
  type,
}: {
  type: DestinationTypeReference;
}) => {
  return <Markdown>{type.instructions}</Markdown>;
};

export default ConfigurationGuide;
