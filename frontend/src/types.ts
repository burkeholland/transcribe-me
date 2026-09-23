export interface FileInfo {
  path: string;
  name: string;
  size: number;
  durationMs: number;
  height?: number;
}

export interface Summary {
  id: string;
  fileName: string;
  createdAt: string;
  language: string;
  durationMs: number;
  wordCount: number;
  sourcePath?: string;
  size?: number;
  height?: number;
}

export interface Transcript extends Summary {
  text: string;
  segments: { startMs: number; endMs: number; text: string }[];
}

export interface Job {
  id: string;
  state: 'preparing' | 'transcribing' | 'completed' | 'cancelled' | 'failed';
  progress: number;
  message: string;
  fileName: string;
  error: string;
  transcriptID: string;
}

export interface Snapshot {
  ready: boolean;
  setupError: string;
  runtimeState: 'checking' | 'required' | 'downloading' | 'installing' | 'ready' | 'failed';
  runtimeMessage: string;
  runtimeError: string;
  runtimeProgress: number;
  runtimeDownloadedBytes: number;
  runtimeDownloadTotalBytes: number;
  runtimeTotalBytes: number;
  modelName: string;
  version: string;
  job: Job | null;
  history: Summary[];
  historyWarning?: string;
}

export interface AppBridge {
  Status(): Promise<Snapshot>;
  InstallModels(): Promise<void>;
  ChooseFile(): Promise<FileInfo | null>;
  InspectFile(path: string): Promise<FileInfo>;
  StartTranscription(path: string, language: string): Promise<void>;
  Cancel(): Promise<void>;
  GetTranscript(id: string): Promise<Transcript>;
  ExportTranscript(id: string, format: string): Promise<string>;
  CopyTranscript(id: string): Promise<void>;
  MinimiseWindow?(): Promise<void>;
  ToggleMaximiseWindow?(): Promise<void>;
  CloseWindow?(): Promise<void>;
}

declare global {
  interface Window {
    go?: { main?: { App?: AppBridge } };
    runtime?: {
      OnFileDrop?: (callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean) => void;
      OnFileDropOff?: () => void;
    };
  }
}
