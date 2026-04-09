// Wraps the app with AssistantUI's runtime context and manages the WebSocket lifecycle.
// Lives in AppShell so the WebSocket stays connected across all screens.

import { useEffect, useRef } from "react";
import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { useOmnipusRuntime } from "@/lib/omnipus-runtime";
import { useChatStore } from "@/store/chat";
import { useConnectionStore } from "@/store/connection";
import { WsConnection } from "@/lib/ws";
import { TerminalOutputUI } from "./tools/TerminalOutput";
import { FileReadPreviewUI, FileReadAliasDotUI } from "./tools/FileReadPreview";
import { FileWriteConfirmUI, FileWriteAliasDotUI, EditFileConfirmUI, AppendFileConfirmUI } from "./tools/FileWriteConfirm";
import { FileTreeViewUI, FileListAliasDotUI } from "./tools/FileTreeView";
import { WebSearchResultUI } from "./tools/WebSearchResult";
import { WebFetchPreviewUI } from "./tools/WebFetchPreview";
import { BrowserNavigateUI, BrowserNavigateUnderscoreUI } from "./tools/BrowserNavigate";

// Manages WebSocket connection lifecycle — renders nothing, only side effects.
function WsLifecycle() {
  const handleFrame = useChatStore((s) => s.handleFrame);
  const setConnection = useConnectionStore((s) => s.setConnection);
  const setConnected = useConnectionStore((s) => s.setConnected);
  const setConnectionError = useConnectionStore((s) => s.setConnectionError);
  const connectionRef = useRef<WsConnection | null>(null);

  useEffect(() => {
    const conn = new WsConnection({
      onFrame: handleFrame,
      onConnected: async () => {
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
       * Tool name → UI component mapping. Underscore names match pkg/sysagent/tools/ exports
       * (PicoClaw convention); dot-notation names match BRD C.6.1.4 spec. Both registered
       * to handle either naming convention from the agent.
       *   exec              → TerminalOutputUI          (shell command execution)
       *   read_file         → FileReadPreviewUI         (read file content)
       *   file.read         → FileReadAliasDotUI        (BRD alias)
       *   write_file        → FileWriteConfirmUI        (create/overwrite file)
       *   file.write        → FileWriteAliasDotUI       (BRD alias)
       *   edit_file         → EditFileConfirmUI         (targeted string replacement)
       *   append_file       → AppendFileConfirmUI       (append to file)
       *   list_dir          → FileTreeViewUI            (directory listing)
       *   file.list         → FileListAliasDotUI        (BRD alias)
       *   web_search        → WebSearchResultUI         (search the web)
       *   web_fetch         → WebFetchPreviewUI         (fetch a URL)
       *   browser.navigate  → BrowserNavigateUI         (browser navigation + screenshot)
       *   browser_navigate  → BrowserNavigateUnderscoreUI (underscore variant)
       */}
      <TerminalOutputUI />
      <FileReadPreviewUI />
      <FileReadAliasDotUI />
      <FileWriteConfirmUI />
      <FileWriteAliasDotUI />
      <EditFileConfirmUI />
      <AppendFileConfirmUI />
      <FileTreeViewUI />
      <FileListAliasDotUI />
      <WebSearchResultUI />
      <WebFetchPreviewUI />
      <BrowserNavigateUI />
      <BrowserNavigateUnderscoreUI />
      <WsLifecycle />
      {children}
    </AssistantRuntimeProvider>
  );
}
