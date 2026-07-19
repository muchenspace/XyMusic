package com.xymusic.app.data.sync

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.workDataOf
import com.xymusic.app.core.sync.PendingSyncScheduler
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class WorkManagerPendingSyncScheduler
@Inject
constructor(@ApplicationContext private val context: Context) :
    PendingSyncScheduler {
    override fun schedule(ownerUserId: String) {
        enqueue(ownerUserId, ExistingWorkPolicy.KEEP)
    }

    override fun continueDrain(ownerUserId: String) {
        enqueue(ownerUserId, ExistingWorkPolicy.APPEND_OR_REPLACE)
    }

    private fun enqueue(ownerUserId: String, policy: ExistingWorkPolicy) {
        val request =
            OneTimeWorkRequestBuilder<PendingSyncWorker>()
                .setInputData(workDataOf(PendingSyncWorker.KEY_OWNER_USER_ID to ownerUserId))
                .setConstraints(
                    Constraints
                        .Builder()
                        .setRequiredNetworkType(NetworkType.CONNECTED)
                        .build(),
                ).setBackoffCriteria(
                    BackoffPolicy.EXPONENTIAL,
                    INITIAL_BACKOFF_SECONDS,
                    TimeUnit.SECONDS,
                ).addTag(tag(ownerUserId))
                .build()
        WorkManager.getInstance(context).enqueueUniqueWork(
            uniqueWorkName(ownerUserId),
            policy,
            request,
        )
    }

    override fun cancel(ownerUserId: String) {
        WorkManager.getInstance(context).cancelUniqueWork(uniqueWorkName(ownerUserId))
    }

    private fun uniqueWorkName(ownerUserId: String) = "pending-sync-$ownerUserId"

    private fun tag(ownerUserId: String) = "pending-sync-owner-$ownerUserId"

    private companion object {
        const val INITIAL_BACKOFF_SECONDS = 30L
    }
}
