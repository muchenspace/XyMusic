package com.xymusic.app.feature.player.presentation

import com.google.common.truth.Truth.assertThat
import org.junit.Test

/** Pure state-machine checks for lyric transitions that run on every frame. */
class PlayerLyricsAnimationStateContractTest {
    @Test
    fun denseJumpOnlyEmphasizesItsSourceAndTargetLines() {
        val transition = LyricEmphasisTransition.settled(2).retarget(
            emphasisPhase = 0f,
            targetLineIndex = 5,
        )
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 1f / 3f,
                lineIndex = 3,
                transition = transition,
            ),
        ).isEqualTo(0f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 2f / 3f,
                lineIndex = 4,
                transition = transition,
            ),
        ).isEqualTo(0f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 0.5f,
                lineIndex = 2,
                transition = transition,
            ),
        ).isWithin(0.001f).of(0.5f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 0.5f,
                lineIndex = 5,
                transition = transition,
            ),
        ).isWithin(0.001f).of(0.5f)
    }

    @Test
    fun backwardAdjacentPlaybackJumpUsesSnapInsteadOfReverseAnimation() {
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 5,
                lyricIndex = 4,
                previousLyricTimeMs = 5_000L,
                lyricTimeMs = 4_000L,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
    }

    @Test
    fun backwardAdjacentJumpWithoutTimestampsAlsoUsesSnap() {
        assertThat(
            lyricFollowScrollMode(
                previousLyricIndex = 5,
                lyricIndex = 4,
            ),
        ).isEqualTo(LyricFollowScrollMode.Snap)
    }

    @Test
    fun transitionEmphasisHandlesEndpointsBackwardMotionAndInvalidPositions() {
        val transition = LyricEmphasisTransition.settled(5).retarget(
            emphasisPhase = 0f,
            targetLineIndex = 2,
        )
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 0f,
                lineIndex = 5,
                transition = transition,
            ),
        ).isEqualTo(1f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 0.5f,
                lineIndex = 2,
                transition = transition,
            ),
        ).isWithin(0.001f).of(0.5f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 1f,
                lineIndex = 5,
                transition = transition,
            ),
        ).isEqualTo(0f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = Float.NaN,
                lineIndex = 2,
                transition = transition,
            ),
        ).isEqualTo(0f)
        assertThat(
            lyricLineTransitionEmphasis(
                emphasisPhase = 0.5f,
                lineIndex = 0,
                transition = LyricEmphasisTransition.Empty,
            ),
        ).isEqualTo(0f)
    }

    @Test
    fun interruptedTransitionPreservesEveryVisibleWeightBeforeRetargeting() {
        val firstTransition = LyricEmphasisTransition.settled(4).retarget(
            emphasisPhase = 0f,
            targetLineIndex = 5,
        )
        val interruptedPhase = 0.5f
        val retargeted = firstTransition.retarget(
            emphasisPhase = interruptedPhase,
            targetLineIndex = 6,
        )

        assertThat(retargeted.emphasisAt(interruptedPhase, 4)).isWithin(0.001f).of(0.5f)
        assertThat(retargeted.emphasisAt(interruptedPhase, 5)).isWithin(0.001f).of(0.5f)
        assertThat(retargeted.emphasisAt(interruptedPhase, 6)).isEqualTo(0f)

        val halfwayToNewTarget = 1f
        assertThat(retargeted.emphasisAt(halfwayToNewTarget, 4)).isWithin(0.001f).of(0.25f)
        assertThat(retargeted.emphasisAt(halfwayToNewTarget, 5)).isWithin(0.001f).of(0.25f)
        assertThat(retargeted.emphasisAt(halfwayToNewTarget, 6)).isWithin(0.001f).of(0.5f)
        assertThat(retargeted.emphasisAt(halfwayToNewTarget, 3)).isEqualTo(0f)
    }

    @Test
    fun sameLineSnapshotResumesAnInterruptedEmphasisTransition() {
        val interrupted = LyricEmphasisTransition.settled(4).retarget(
            emphasisPhase = 0f,
            targetLineIndex = 5,
        )

        assertThat(
            lyricTransitionNeedsSettling(
                animatedLinePosition = 4.4f,
                emphasisPhase = 0.4f,
                lineIndex = 5,
                transition = interrupted,
            ),
        ).isTrue()
        assertThat(
            lyricTransitionNeedsSettling(
                animatedLinePosition = 5f,
                emphasisPhase = 0f,
                lineIndex = 5,
                transition = LyricEmphasisTransition.settled(5),
            ),
        ).isFalse()
    }

    @Test
    fun repeatedRetargetingPreservesEveryVisibleWeightAtTheRetargetFrame() {
        var transition = LyricEmphasisTransition.settled(0)
        var position = 0f
        repeat(100) { target ->
            position += 0.01f
            val visibleLines = transition.startEmphasis.keys + transition.targetLineIndex
            val expectedWeights = visibleLines.associateWith { line -> transition.emphasisAt(position, line) }
            val retargeted = transition.retarget(emphasisPhase = position, targetLineIndex = target + 1)

            expectedWeights.forEach { (line, expectedWeight) ->
                assertThat(retargeted.emphasisAt(position, line)).isWithin(0.000001f).of(expectedWeight)
            }
            transition = retargeted
        }
    }

    @Test
    fun pathologicalRetargetingKeepsTheSparseWeightMapBounded() {
        var transition = LyricEmphasisTransition.settled(0)
        var phase = 0f
        repeat(10_000) { target ->
            phase += 0.01f
            transition = transition.retarget(emphasisPhase = phase, targetLineIndex = target + 1)
        }

        assertThat(transition.startEmphasis.size).isAtMost(1_024)
    }

    @Test
    fun retargetAtTheSameScrollPositionStillPreservesAndAnimatesWeights() {
        val interrupted = LyricEmphasisTransition.settled(2)
            .retarget(emphasisPhase = 0f, targetLineIndex = 5)
            .retarget(emphasisPhase = 1f / 3f, targetLineIndex = 3)

        assertThat(interrupted.emphasisAt(1f / 3f, 2)).isWithin(0.001f).of(2f / 3f)
        assertThat(interrupted.emphasisAt(1f / 3f, 5)).isWithin(0.001f).of(1f / 3f)
        assertThat(interrupted.emphasisAt(1f / 3f, 3)).isEqualTo(0f)

        assertThat(interrupted.emphasisAt(5f / 6f, 2)).isWithin(0.001f).of(1f / 3f)
        assertThat(interrupted.emphasisAt(5f / 6f, 5)).isWithin(0.001f).of(1f / 6f)
        assertThat(interrupted.emphasisAt(5f / 6f, 3)).isWithin(0.001f).of(0.5f)
    }

    @Test
    fun nanLayoutDeltaNeverCountsAsSettled() {
        assertThat(
            lyricLayoutDeltaHasSettled(
                previousDelta = Float.NaN,
                currentDelta = Float.NaN,
            ),
        ).isFalse()
        assertThat(
            lyricLayoutDeltaHasSettled(
                previousDelta = 1f,
                currentDelta = Float.NaN,
            ),
        ).isFalse()
    }
}
