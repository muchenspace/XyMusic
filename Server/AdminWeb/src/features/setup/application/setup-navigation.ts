export function canAdvanceSetupStep(input: {
  validated: boolean;
  expectedIndex: number;
  currentIndex: number;
  expectedRevision: number;
  currentRevision: number;
}): boolean {
  return input.validated &&
    input.expectedIndex === input.currentIndex &&
    input.expectedRevision === input.currentRevision;
}
