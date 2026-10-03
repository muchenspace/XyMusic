package com.xymusic.app.data.sync

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.workDataOf
import com.xymusic.app.core.common.IoDispatcher
import com.xymusic.app.core.sync.PendingSyncScheduler
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

@Singleton
class WorkManagerPendingSyncScheduler
@Inject
constructor(
    @ApplicationContext private val context: Context,
    @IoDispatcher ioDispatcher: CoroutineDispatcher,
) : PendingSyncScheduler {
    // Callers include main-thread paths (session restore and sign-in), and the
    // first WorkManager.getInstance() opens its own database. Enqueueing off the
    // main thread keeps that cost out of startup and out of the session callbacks.
    private val scope = CoroutineScope(SupervisorJob() + ioDispatcher)

    override fun schedule(ownerUserId: String) {
        enqueue(ownerUserId, ExistingWorkPolicy.KEEP)
    }

    override fun continueDrain(ownerUserId: String) {
        enqueue(ownerUserId, ExistingWorkPolicy.APPEND_OR_REPLACE)
    }

    private fun enqueue(ownerUserId: String, policy: ExistingWorkPolicy) {
        scope.launch {
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
    }

    override fun cancel(ownerUserId: String) {
        scope.launch {
            WorkManager.getInstance(context).cancelUniqueWork(uniqueWorkName(ownerUserId))
        }
    }

    private fun uniqueWorkName(ownerUserId: String) = "pending-sync-$ownerUserId"

    private fun tag(ownerUserId: String) = "pending-sync-owner-$ownerUserId"

    private companion object {
        const val INITIAL_BACKOFF_SECONDS = 30L
    }
}
