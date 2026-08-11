import { useCallback, useRef, useState } from "react";
import type { Annotation } from "./types";

const same = (left: Annotation[], right: Annotation[]) => JSON.stringify(left) === JSON.stringify(right);

export function useEditorHistory(initial: Annotation[] = []) {
  const [past, setPast] = useState<Annotation[][]>([]);
  const [present, setPresent] = useState<Annotation[]>(initial);
  const [future, setFuture] = useState<Annotation[][]>([]);
  const checkpoint = useRef<Annotation[] | null>(null);

  const commit = useCallback(
    (next: Annotation[]) => {
      if (same(present, next)) return;
      setPast((items) => [...items.slice(-79), present]);
      setPresent(next);
      setFuture([]);
    },
    [present],
  );

  const beginTransaction = useCallback(() => {
    checkpoint.current = present;
  }, [present]);

  const endTransaction = useCallback(
    (next: Annotation[]) => {
      const start = checkpoint.current;
      checkpoint.current = null;
      if (!start || same(start, next)) return;
      setPast((items) => [...items.slice(-79), start]);
      setPresent(next);
      setFuture([]);
    },
    [],
  );

  const undo = useCallback(() => {
    setPast((items) => {
      const previous = items.at(-1);
      if (!previous) return items;
      setFuture((next) => [present, ...next].slice(0, 80));
      setPresent(previous);
      return items.slice(0, -1);
    });
  }, [present]);

  const redo = useCallback(() => {
    setFuture((items) => {
      const next = items[0];
      if (!next) return items;
      setPast((previous) => [...previous.slice(-79), present]);
      setPresent(next);
      return items.slice(1);
    });
  }, [present]);

  const reset = useCallback((next: Annotation[] = []) => {
    checkpoint.current = null;
    setPast([]);
    setPresent(next);
    setFuture([]);
  }, []);

  return {
    annotations: present,
    setTransient: setPresent,
    commit,
    beginTransaction,
    endTransaction,
    undo,
    redo,
    reset,
    canUndo: past.length > 0,
    canRedo: future.length > 0,
  };
}
