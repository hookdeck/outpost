import {
  createContext,
  useContext,
  useState,
  useEffect,
  ReactNode,
  useCallback,
} from "react";
import { createPortal } from "react-dom";
import { CollapseIcon } from "../Icons";
import Button from "../Button/Button";
import "./Sidebar.scss";

type SidebarState = {
  id: string;
  content: ReactNode;
} | null;

type SidebarContextType = {
  open: (id: string, content: ReactNode) => void;
  close: (id: string) => void;
  toggle: (id: string, content: ReactNode) => void;
};

const SidebarContext = createContext<SidebarContextType | null>(null);

export const SidebarProvider = ({ children }: { children: ReactNode }) => {
  const [state, setState] = useState<SidebarState>(null);

  const open = useCallback((id: string, content: ReactNode) => {
    setState({ id, content });
  }, []);

  const close = useCallback((id: string) => {
    setState((current) => (current?.id === id ? null : current));
  }, []);

  const toggle = useCallback((id: string, content: ReactNode) => {
    setState((current) => {
      if (current?.id === id) {
        return null;
      }
      return { id, content };
    });
  }, []);

  const handleClose = useCallback(() => {
    setState(null);
  }, []);

  return (
    <SidebarContext.Provider value={{ open, close, toggle }}>
      {children}
      <SidebarPortal state={state} onClose={handleClose} />
    </SidebarContext.Provider>
  );
};

const SidebarPortal = ({
  state,
  onClose,
}: {
  state: SidebarState;
  onClose: () => void;
}) => {
  const isOpen = state !== null;

  useEffect(() => {
    if (isOpen) {
      document.body.classList.add("sidebar-open");
    } else {
      document.body.classList.remove("sidebar-open");
    }
  }, [isOpen]);

  if (!state) {
    return null;
  }

  return createPortal(
    <div id="sidebar">
      <Button minimal onClick={onClose} className="close-button">
        <CollapseIcon />
      </Button>
      {state.content}
    </div>,
    document.body,
  );
};

export const useSidebar = () => {
  const context = useContext(SidebarContext);
  if (!context) {
    throw new Error("useSidebar must be used within a SidebarProvider");
  }
  return context;
};
