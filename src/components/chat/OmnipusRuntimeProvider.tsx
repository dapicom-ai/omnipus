// Wraps the app with AssistantUI's runtime context and manages the WebSocket lifecycle.
// Lives in AppShell so the WebSocket stays connected across all screens.

import { useEffect, useRef } from "react";
import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { useOmnipusRuntime } from "@/lib/omnipus-runtime";
import { useChatStore } from "@/store/chat";
import { WsConnection } from "@/lib/ws";
import { TerminalOutputUI } from "./tools/TerminalOutput";
import { FileReadPreviewUI } from "./tools/FileReadPreview";
import { FileWriteConfirmUI, EditFileConfirmUI, AppendFileConfirmUI } from "./tools/FileWriteConfirm";
import { FileTreeViewUI } from "./tools/FileTreeView";
import { WebSearchResultUI } from "./tools/WebSearchResult";
import { WebFetchPreviewUI } from "./tools/WebFetchPreview";

// Manages WebSocket connection lifecycle — renders nothing, only side effects.
function WsLifecycle() {
  const handleFrame = useChatStore((s) => s.handleFrame);
  const setConnection = useChatStore((s) => s.setConnection);
  const setConnected = useChatStore((s) => s.setConnected);
  const setConnectionError = useChatStore((s) => s.setConnectionError);
  const connectionRef = useRef<WsConnection | null>(null);

  useEffect(() => {
    const conn = new WsConnection({
      onFrame: handleFrame,
      onConnected: () => {
        setConnected(true);
        setConnectionError(null);
      },
      onDisconnected: () => setConnected(false),
      onError: setConnectionError,
    });
    conn.connect();
    connectionRef.current = conn;
    setConnection(conn);

    return () => {
      conn.disconnect();
      connectionRef.current = null;
      setConnection(null);
    };
    // Store methods are stable Zustand references — intentional empty deps
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return null;
}

export function OmnipusRuntimeProvider({ children }: { children: React.ReactNode }) {
  const runtime = useOmnipusRuntime();

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      {/*
       * Tool UI registrations — each component calls useAssistantToolUI on mount.
       * Tool name → UI component mapping (names match pkg/sysagent/tools/ exports):
       *   exec         → TerminalOutputUI     (shell command execution)
       *   read_file    → FileReadPreviewUI    (read file content)
       *   write_file   → FileWriteConfirmUI   (create/overwrite file)
       *   edit_file    → EditFileConfirmUI    (targeted string replacement)
       *   append_file  → AppendFileConfirmUI  (append to file)
       *   list_dir     → FileTreeViewUI       (directory listing)
       *   web_search   → WebSearchResultUI    (search the web)
       *   web_fetch    → WebFetchPreviewUI    (fetch a URL)
       */}
      <TerminalOutputUI />
      <FileReadPreviewUI />
      <FileWriteConfirmUI />
      <EditFileConfirmUI />
      <AppendFileConfirmUI />
      <FileTreeViewUI />
      <WebSearchResultUI />
      <WebFetchPreviewUI />
      <WsLifecycle />
      {children}
    </AssistantRuntimeProvider>
  );
}
