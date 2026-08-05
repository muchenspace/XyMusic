import type { ConcretePlaybackQuality, PlaybackQuality } from "../../domain/music";

const QUALITY_ORDER: readonly ConcretePlaybackQuality[] = [
  "DATA_SAVER",
  "STANDARD",
  "HIGH",
  "LOSSLESS",
];

const REQUIRED_BANDWIDTH: Readonly<Record<ConcretePlaybackQuality, number>> = {
  DATA_SAVER: 100_000,
  STANDARD: 200_000,
  HIGH: 500_000,
  LOSSLESS: 1_600_000,
};

export interface PlaybackBandwidthSample {
  bitsPerSecond: number;
  durationMs: number;
}

export class AutomaticQualityController {
  private fastEstimate = 0;
  private slowEstimate = 0;
  private sampleCount = 0;
  private activeQuality: ConcretePlaybackQuality = "STANDARD";
  private activeAutomatic = false;
  private activeTrackSampleCount = 0;
  private activeTrackStalled = false;
  private consecutiveStableTracks = 0;
  private qualityCeiling: ConcretePlaybackQuality | null = null;
  private lastAutomaticQuality: ConcretePlaybackQuality = "STANDARD";
  private lastDowngradeAt = Number.NEGATIVE_INFINITY;

  observe(sample: PlaybackBandwidthSample): void {
    if (!Number.isFinite(sample.bitsPerSecond)
      || sample.bitsPerSecond < MIN_SAMPLE_BPS
      || sample.bitsPerSecond > MAX_SAMPLE_BPS
      || !Number.isFinite(sample.durationMs)
      || sample.durationMs < MIN_SAMPLE_DURATION_MS) return;

    if (this.sampleCount === 0) {
      this.fastEstimate = sample.bitsPerSecond;
      this.slowEstimate = sample.bitsPerSecond;
    } else {
      this.fastEstimate = FAST_SAMPLE_WEIGHT * sample.bitsPerSecond
        + (1 - FAST_SAMPLE_WEIGHT) * this.fastEstimate;
      this.slowEstimate = SLOW_SAMPLE_WEIGHT * sample.bitsPerSecond
        + (1 - SLOW_SAMPLE_WEIGHT) * this.slowEstimate;
    }
    this.sampleCount += 1;
  }

  hasReliableEstimate(): boolean {
    return this.sampleCount >= REQUIRED_SAMPLE_COUNT;
  }

  selectTrackQuality(preference: PlaybackQuality): ConcretePlaybackQuality {
    if (preference !== "AUTO") return preference;
    if (!this.hasReliableEstimate()) return BOOTSTRAP_QUALITY;

    let candidate = this.qualityForBandwidth(this.safeBandwidth(SAFETY_FACTOR));
    if (this.qualityCeiling && rank(candidate) > rank(this.qualityCeiling)) {
      candidate = this.qualityCeiling;
    }

    if (rank(candidate) > rank(this.lastAutomaticQuality)) {
      if (this.consecutiveStableTracks < STABLE_TRACKS_FOR_UPGRADE) {
        return this.lastAutomaticQuality;
      }
      return nextHigher(this.lastAutomaticQuality);
    }
    return candidate;
  }

  beginTrack(quality: ConcretePlaybackQuality, automatic: boolean): void {
    this.activeQuality = quality;
    this.activeAutomatic = automatic;
    this.activeTrackSampleCount = this.sampleCount;
    this.activeTrackStalled = false;
    if (automatic) {
      if (quality !== this.lastAutomaticQuality) this.consecutiveStableTracks = 0;
      this.lastAutomaticQuality = quality;
    }
  }

  applySelectedQuality(quality: ConcretePlaybackQuality): void {
    this.activeQuality = quality;
    if (this.activeAutomatic) {
      if (quality !== this.lastAutomaticQuality) this.consecutiveStableTracks = 0;
      this.lastAutomaticQuality = quality;
    }
  }

  finishTrack(): void {
    if (!this.activeAutomatic) return;
    if (this.activeTrackStalled) {
      this.consecutiveStableTracks = 0;
    } else if (this.sampleCount > this.activeTrackSampleCount) {
      this.consecutiveStableTracks += 1;
      if (this.qualityCeiling && this.consecutiveStableTracks >= STABLE_TRACKS_FOR_UPGRADE) {
        this.qualityCeiling = nextHigher(this.qualityCeiling);
      }
    }
    this.activeAutomatic = false;
  }

  handleRebuffer(nowMs: number): ConcretePlaybackQuality | null {
    if (!this.activeAutomatic || rank(this.activeQuality) === 0) return null;
    if (!Number.isFinite(nowMs) || nowMs - this.lastDowngradeAt < DOWNGRADE_COOLDOWN_MS) return null;

    this.lastDowngradeAt = nowMs;
    this.activeTrackStalled = true;
    this.consecutiveStableTracks = 0;
    const oneStepLower = QUALITY_ORDER[rank(this.activeQuality) - 1]!;
    const bandwidthTarget = this.qualityForBandwidth(this.safeBandwidth(REBUFFER_SAFETY_FACTOR));
    const target = rank(bandwidthTarget) < rank(oneStepLower) ? bandwidthTarget : oneStepLower;
    this.activeQuality = target;
    this.lastAutomaticQuality = target;
    this.qualityCeiling = target;
    return target;
  }

  resetNetworkEstimate(): void {
    this.fastEstimate = 0;
    this.slowEstimate = 0;
    this.sampleCount = 0;
    this.consecutiveStableTracks = 0;
    this.qualityCeiling = null;
    this.lastAutomaticQuality = BOOTSTRAP_QUALITY;
  }

  resetSession(): void {
    this.resetNetworkEstimate();
    this.activeQuality = BOOTSTRAP_QUALITY;
    this.activeAutomatic = false;
    this.activeTrackSampleCount = 0;
    this.activeTrackStalled = false;
    this.lastDowngradeAt = Number.NEGATIVE_INFINITY;
  }

  private safeBandwidth(factor: number): number {
    if (!this.hasReliableEstimate()) return 0;
    return Math.min(this.fastEstimate, this.slowEstimate) * factor;
  }

  private qualityForBandwidth(bitsPerSecond: number): ConcretePlaybackQuality {
    let selected: ConcretePlaybackQuality = "DATA_SAVER";
    for (const quality of QUALITY_ORDER) {
      if (bitsPerSecond < REQUIRED_BANDWIDTH[quality]) break;
      selected = quality;
    }
    return selected;
  }
}

function rank(quality: ConcretePlaybackQuality): number {
  return QUALITY_ORDER.indexOf(quality);
}

function nextHigher(quality: ConcretePlaybackQuality): ConcretePlaybackQuality {
  return QUALITY_ORDER[Math.min(QUALITY_ORDER.length - 1, rank(quality) + 1)]!;
}

const BOOTSTRAP_QUALITY: ConcretePlaybackQuality = "STANDARD";
const REQUIRED_SAMPLE_COUNT = 2;
const STABLE_TRACKS_FOR_UPGRADE = 2;
const MIN_SAMPLE_DURATION_MS = 200;
const MIN_SAMPLE_BPS = 16_000;
const MAX_SAMPLE_BPS = 100_000_000;
const FAST_SAMPLE_WEIGHT = 0.5;
const SLOW_SAMPLE_WEIGHT = 0.2;
const SAFETY_FACTOR = 0.7;
const REBUFFER_SAFETY_FACTOR = 0.6;
const DOWNGRADE_COOLDOWN_MS = 15_000;
