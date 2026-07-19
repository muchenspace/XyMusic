package com.xymusic.app.feature.auth.presentation

import app.cash.turbine.test
import com.google.common.truth.Truth.assertThat
import com.xymusic.app.core.network.DomainError
import com.xymusic.app.core.network.NetworkFailureReason
import com.xymusic.app.core.network.model.ProblemCode
import com.xymusic.app.feature.auth.domain.AuthRepository
import com.xymusic.app.feature.auth.domain.AuthResult
import com.xymusic.app.feature.auth.domain.AuthUseCases
import com.xymusic.app.feature.auth.domain.model.LoginCommand
import com.xymusic.app.feature.auth.domain.model.RegisterCommand
import com.xymusic.app.feature.auth.domain.model.RegistrationResult
import com.xymusic.app.support.MainDispatcherRule
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AuthViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun invalidRegistrationIsRejectedBeforeRepositoryCall() = runTest {
        val repository = FakeAuthRepository()
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.register(
            username = "a",
            password = "short",
            confirmPassword = "different",
        )

        assertThat(repository.registerCalls).isEqualTo(0)
        assertThat(viewModel.uiState.value.fieldErrors.keys).containsAtLeast(
            AuthField.Username,
            AuthField.Password,
            AuthField.ConfirmPassword,
        )
        assertThat(viewModel.uiState.value.isSubmitting).isFalse()
    }

    @Test
    fun successfulRegistrationReturnsToSignIn() = runTest {
        val repository = FakeAuthRepository()
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.effects.test {
            viewModel.register(
                username = "alice_01",
                password = "password-123",
                confirmPassword = "password-123",
            )
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                AuthEffect.NavigateToSignIn(AuthMessage.AccountCreated),
            )
            assertThat(viewModel.uiState.value.isSubmitting).isFalse()
        }
    }

    @Test
    fun disabledRegistrationShowsServerReasonAndTraceId() = runTest {
        val repository = FakeAuthRepository().apply {
            registerHandler = {
                AuthResult.Failure(
                    DomainError.PermissionDenied(
                        detail = "当前服务器未开放用户注册，请联系管理员开启注册功能。",
                        traceId = "trace-registration-disabled",
                        reason = ProblemCode.Forbidden,
                    ),
                )
            }
        }
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.effects.test {
            viewModel.register("alice_01", "password-123", "password-123")
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                AuthEffect.ShowMessage(
                    AuthMessage.Server("当前服务器未开放用户注册，请联系管理员开启注册功能。（追踪 ID：trace-registration-disabled）"),
                ),
            )
        }
    }

    @Test
    fun serverRegistrationFailureShowsDetailAndTraceId() = runTest {
        val repository = FakeAuthRepository().apply {
            registerHandler = {
                AuthResult.Failure(
                    DomainError.Server(
                        detail = "账号创建未完成，请稍后重试。",
                        traceId = "trace-registration-error",
                    ),
                )
            }
        }
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.effects.test {
            viewModel.register("alice_01", "password-123", "password-123")
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                AuthEffect.ShowMessage(
                    AuthMessage.Server("账号创建未完成，请稍后重试。（追踪 ID：trace-registration-error）"),
                ),
            )
        }
    }

    @Test
    fun unexpectedRepositoryFailureShowsGenericMessageAndEndsSubmission() = runTest {
        val repository =
            FakeAuthRepository().apply {
                registerHandler = { error("database implementation detail") }
            }
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.effects.test {
            viewModel.register(
                username = "alice_01",
                password = "password-123",
                confirmPassword = "password-123",
            )
            advanceUntilIdle()

            assertThat(awaitItem()).isEqualTo(
                AuthEffect.ShowMessage(AuthMessage.GenericError),
            )
            assertThat(viewModel.uiState.value.isSubmitting).isFalse()
        }
    }

    @Test
    fun invalidUsernameLoginIsRejectedBeforeRepositoryCall() = runTest {
        val repository = FakeAuthRepository()
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.login("invalid username", "password-123")

        assertThat(repository.loginCalls).isEqualTo(0)
        assertThat(viewModel.uiState.value.fieldErrors[AuthField.Username])
            .isEqualTo(AuthFieldError.InvalidUsername)
    }

    @Test
    fun loginFailuresMapToSpecificMessages() = runTest {
        val cases =
            listOf(
                DomainError.Network(
                    detail = "Server refused the connection",
                    reason = NetworkFailureReason.ConnectionRefused,
                ) to AuthMessage.NetworkFailure(NetworkFailureReason.ConnectionRefused),
                DomainError.ServiceUnavailable("Service unavailable", null) to AuthMessage.ServerUnavailable,
                DomainError.NotFound("Resource not found", null) to AuthMessage.LoginEndpointNotFound,
                DomainError.Validation("Invalid input", null, emptyMap()) to AuthMessage.InvalidInput,
                DomainError.Protocol("Invalid response", null, null) to AuthMessage.ProtocolError,
                DomainError.Local("Storage unavailable") to AuthMessage.LocalDataError,
                DomainError.Authentication(
                    "Invalid credentials",
                    null,
                    ProblemCode.InvalidCredentials,
                ) to AuthMessage.InvalidCredentials,
            )

        cases.forEach { (error, expectedMessage) ->
            val repository = FakeAuthRepository().apply { loginResult = AuthResult.Failure(error) }
            val viewModel = AuthViewModel(AuthUseCases(repository))

            viewModel.effects.test {
                viewModel.login("alice_01", "password-123")
                advanceUntilIdle()

                assertThat(awaitItem()).isEqualTo(AuthEffect.ShowMessage(expectedMessage))
                assertThat(viewModel.uiState.value.isSubmitting).isFalse()
            }
        }
    }

    @Test
    fun concurrentSubmissionsDoNotCreateDuplicateRegistrationCalls() = runTest {
        val pendingResult = CompletableDeferred<AuthResult<RegistrationResult>>()
        val repository =
            FakeAuthRepository().apply {
                registerHandler = { pendingResult.await() }
            }
        val viewModel = AuthViewModel(AuthUseCases(repository))

        viewModel.register(
            "alice_01",
            "password-123",
            "password-123",
        )
        advanceUntilIdle()
        viewModel.register(
            "alice_01",
            "password-123",
            "password-123",
        )

        assertThat(repository.registerCalls).isEqualTo(1)
        assertThat(viewModel.uiState.value.isSubmitting).isTrue()

        pendingResult.complete(AuthResult.Success(registrationResult()))
        advanceUntilIdle()
        assertThat(viewModel.uiState.value.isSubmitting).isFalse()
    }

    private class FakeAuthRepository : AuthRepository {
        var registerCalls = 0
        var loginCalls = 0
        var registerHandler: suspend (RegisterCommand) -> AuthResult<RegistrationResult> = {
            AuthResult.Success(registrationResult())
        }
        var loginResult: AuthResult<Unit> = AuthResult.Success(Unit)

        override suspend fun register(command: RegisterCommand): AuthResult<RegistrationResult> {
            registerCalls += 1
            return registerHandler(command)
        }

        override suspend fun login(command: LoginCommand): AuthResult<Unit> {
            loginCalls += 1
            return loginResult
        }

        override suspend fun logout(): AuthResult<Unit> = AuthResult.Success(Unit)
    }

    private companion object {
        fun registrationResult() = RegistrationResult(
            userId = "user-1",
            username = "alice_01",
        )
    }
}
