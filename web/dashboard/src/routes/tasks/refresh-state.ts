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

export const createMutationRevision = () => {
  let revision = 0;

  return {
    current: () => revision,
    bump: () => {
      revision += 1;
      return revision;
    },
    matches: (candidate: number) => candidate === revision
  };
};
