import { useCallback, useRef, useState } from "react";
import type { Annotation } from "./types";
import { sameAnnotations as same } from "./types";

export function useEditorHistory(initial: Annotation[] = []) {
  const [past, setPast] = useState<Annotation[][]>([]);
  const [present, setPresent] = useState<Annotation[]>(initial);
  const [future, setFuture] = useState<Annotation[][]>([]);
  const checkpoint = useRef<Annotation[] | null>(null);
  const presentRef = useRef(present);
  presentRef.current = present;

  const setCurrent = useCallback((next: Annotation[]) => {
    presentRef.current = next;
    setPresent(next);
  }, []);

  const commit = useCallback((next: Annotation[]) => {
    const current = presentRef.current;
    if (same(current, next)) return;
    setPast((items) => [...items.slice(-79), current]);
    setCurrent(next);
    setFuture([]);
  }, [setCurrent]);

  const beginTransaction = useCallback(() => {
    if (!checkpoint.current) checkpoint.current = presentRef.current;
  }, []);

  const endTransaction = useCallback(
    (next: Annotation[] = presentRef.current) => {
      const start = checkpoint.current;
      checkpoint.current = null;
      if (!start || same(start, next)) return;
      setPast((items) => [...items.slice(-79), start]);
      setCurrent(next);
      setFuture([]);
    },
    [setCurrent],
  );

  const setTransient = useCallback((next: Annotation[]) => setCurrent(next), [setCurrent]);

  const undo = useCallback(() => {
    const current = presentRef.current;
    setPast((items) => {
      const previous = items.at(-1);
      if (!previous) return items;
      setFuture((next) => [current, ...next].slice(0, 80));
      setCurrent(previous);
      return items.slice(0, -1);
    });
  }, [setCurrent]);

  const redo = useCallback(() => {
    setFuture((items) => {
      const next = items[0];
      if (!next) return items;
      setPast((previous) => [...previous.slice(-79), presentRef.current]);
      setCurrent(next);
      return items.slice(1);
    });
  }, [setCurrent]);

  const reset = useCallback((next: Annotation[] = []) => {
    checkpoint.current = null;
    setPast([]);
    setCurrent(next);
    setFuture([]);
  }, [setCurrent]);

  return {
    annotations: present,
    setTransient,
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
