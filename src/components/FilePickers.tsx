import type { RefObject } from "react";
import { errorMessage } from "../editor/constants";
import { imageFromFile } from "../platform";
import type { CaptureResult, ScreenPinProject } from "../types";

interface FilePickersProps {
  imageInput: RefObject<HTMLInputElement | null>;
  projectInput: RefObject<HTMLInputElement | null>;
  onImage: (capture: CaptureResult, name?: string) => Promise<void>;
  onProject: (project: ScreenPinProject) => Promise<void>;
  onBeforeReplace: () => Promise<boolean>;
  onError: (message: string) => void;
}

export function FilePickers({
  imageInput,
  projectInput,
  onImage,
  onProject,
  onBeforeReplace,
  onError,
}: FilePickersProps) {
  return (
    <>
      <input
        ref={imageInput}
        hidden
        type="file"
        accept="image/png,image/jpeg,image/webp"
        onChange={async (event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (!file) return;
          try {
            if (!(await onBeforeReplace())) return;
            await onImage(await imageFromFile(file), file.name);
          } catch (error) {
            onError(errorMessage(error));
          }
        }}
      />
      <input
        ref={projectInput}
        hidden
        type="file"
        accept=".screenpin,application/json"
        onChange={async (event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (!file) return;
          try {
            if (!(await onBeforeReplace())) return;
            await onProject(JSON.parse(await file.text()));
          } catch (error) {
            onError(errorMessage(error));
          }
        }}
      />
    </>
  );
}
