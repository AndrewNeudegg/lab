export const createCoalescedAsync = <T>() => {
  let inFlight: Promise<T> | undefined;

  return (task: () => Promise<T>) => {
    if (inFlight) {
      return inFlight;
    }

    inFlight = task().finally(() => {
      inFlight = undefined;
    });
    return inFlight;
  };
};
