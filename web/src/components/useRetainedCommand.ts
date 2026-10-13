import { useEffect } from "react";

// A pending or unconfirmed write retains its original key/body while other tabs
// are viewed. The owning record keeps a single navigation blocker.
export function useRetainedCommand(
  retain: ((tab: string, needed: boolean) => void) | undefined,
  key: string,
  needed: boolean,
) {
  useEffect(() => {
    retain?.(key, needed);
  }, [retain, key, needed]);
  useEffect(() => () => retain?.(key, false), [retain, key]);
}
