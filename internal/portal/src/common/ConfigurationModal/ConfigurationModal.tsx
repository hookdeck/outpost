import { useEffect, useRef } from "react";
import { DestinationTypeReference } from "../../typings/Destination";
import { useSidebar } from "../Sidebar/Sidebar";
import Markdown from "react-markdown";

const ConfigurationModal = ({
  type,
  onClose,
}: {
  type: DestinationTypeReference;
  onClose: () => void;
}) => {
  const { open, close } = useSidebar("configuration");
  const initialized = useRef(false);

  useEffect(() => {
    if (!initialized.current) {
      initialized.current = true;
      open(<Markdown>{type.instructions}</Markdown>, onClose);
    }
    return () => close();
     
  }, []);

  return null;
};

export default ConfigurationModal;
