import {
  createContext,
  useContext,
  useState,
  useEffect,
  useRef,
  ReactNode,
  useCallback,
} from "react";
import { createPortal } from "react-dom";
import { CollapseIcon } from "../Icons";
import Button from "../Button/Button";
import "./Sidebar.scss";

type SidebarContextType = {
  open: (id: string, content: ReactNode, onClose: () => void) => void;
  close: (id: string) => void;
};

const SidebarContext = createContext<SidebarContextType | null>(null);

export const SidebarProvider = ({ children }: { children: ReactNode }) => {
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [content, setContent] = useState<ReactNode | null>(null);
  const onCloseCallbackRef = useRef<(() => void) | null>(null);

  const open = useCallback(
    (id: string, newContent: ReactNode, onClose: () => void) => {
      // If there's a different sidebar open, call its onClose first
      if (currentId && currentId !== id && onCloseCallbackRef.current) {
        onCloseCallbackRef.current();
      }
      setCurrentId(id);
      setContent(newContent);
      onCloseCallbackRef.current = onClose;
    },
    [currentId]
  );

  const close = useCallback(
    (id: string) => {
      // Only close if this id owns the current sidebar
      if (currentId === id) {
        setContent(null);
        setCurrentId(null);
        onCloseCallbackRef.current = null;
      }
    },
    [currentId]
  );

  const handleClose = useCallback(() => {
    if (onCloseCallbackRef.current) {
      onCloseCallbackRef.current();
    }
    setContent(null);
    setCurrentId(null);
    onCloseCallbackRef.current = null;
  }, []);

  return (
    <SidebarContext.Provider value={{ open, close }}>
      {children}
      <SidebarPortal content={content} onClose={handleClose} />
    </SidebarContext.Provider>
  );
};

const SidebarPortal = ({
  content,
  onClose,
}: {
  content: ReactNode | null;
  onClose: () => void;
}) => {
  const isOpen = content !== null;

  useEffect(() => {
    if (isOpen) {
      document.body.classList.add("sidebar-open");
    } else {
      document.body.classList.remove("sidebar-open");
    }
  }, [isOpen]);

  if (!content) {
    return null;
  }

  return createPortal(
    <div id="sidebar">
      <Button minimal onClick={onClose} className="close-button">
        <CollapseIcon />
      </Button>
      {content}
    </div>,
    document.body
  );
};

export const useSidebar = (id: string) => {
  const context = useContext(SidebarContext);
  if (!context) {
    throw new Error("useSidebar must be used within a SidebarProvider");
  }

  const open = useCallback(
    (content: ReactNode, onClose: () => void) => {
      context.open(id, content, onClose);
    },
    [context, id]
  );

  const close = useCallback(() => {
    context.close(id);
  }, [context, id]);

  return { open, close };
};
