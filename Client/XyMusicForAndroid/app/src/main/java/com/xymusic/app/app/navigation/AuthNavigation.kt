package com.xymusic.app.app.navigation

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navigation
import com.xymusic.app.core.network.ServerEndpoint
import com.xymusic.app.feature.auth.presentation.AuthEffect
import com.xymusic.app.feature.auth.presentation.AuthEntryScreen
import com.xymusic.app.feature.auth.presentation.AuthField
import com.xymusic.app.feature.auth.presentation.AuthViewModel
import com.xymusic.app.feature.auth.presentation.RegisterScreen
import com.xymusic.app.feature.auth.presentation.SignInScreen
import com.xymusic.app.feature.auth.presentation.resolve
import com.xymusic.app.feature.server.presentation.ServerEndpointDialog
import com.xymusic.app.ui.theme.slideFadeBackInto
import com.xymusic.app.ui.theme.slideFadeBackOutOf
import com.xymusic.app.ui.theme.slideFadeInto
import com.xymusic.app.ui.theme.slideFadeOutOf

@Composable
fun AuthNavigation(
    snackbarHostState: SnackbarHostState,
    serverEndpoint: ServerEndpoint,
    onServerEndpointChanged: (ServerEndpoint) -> Unit,
    modifier: Modifier = Modifier,
) {
    val navController = rememberNavController()
    var editServer by rememberSaveable { mutableStateOf(false) }
    val currentBackStackEntry by navController.currentBackStackEntryAsState()
    val graphBackStackEntry =
        remember(currentBackStackEntry) {
            currentBackStackEntry?.let {
                runCatching { navController.getBackStackEntry(AuthDestination.Graph.route) }.getOrNull()
            }
        }

    graphBackStackEntry?.let { graphEntry ->
        val viewModel: AuthViewModel = hiltViewModel(graphEntry)
        AuthEffectHandler(
            viewModel = viewModel,
            navController = navController,
            snackbarHostState = snackbarHostState,
        )
    }
    if (editServer) {
        ServerEndpointDialog(
            currentEndpoint = serverEndpoint,
            onDismiss = { editServer = false },
            onSave = { endpoint ->
                editServer = false
                if (endpoint != serverEndpoint) onServerEndpointChanged(endpoint)
            },
        )
    }

    Scaffold(
        modifier = modifier,
        snackbarHost = { SnackbarHost(hostState = snackbarHostState) },
    ) { contentPadding ->
        NavHost(
            navController = navController,
            startDestination = AuthDestination.Graph.route,
            modifier = Modifier.padding(contentPadding),
            enterTransition = { slideFadeInto() },
            exitTransition = { slideFadeOutOf() },
            popEnterTransition = { slideFadeBackInto() },
            popExitTransition = { slideFadeBackOutOf() },
        ) {
            navigation(
                startDestination = AuthDestination.Entry.route,
                route = AuthDestination.Graph.route,
            ) {
                composable(AuthDestination.Entry.route) {
                    val viewModel =
                        authViewModel(
                            navController = navController,
                            backStackEntry = it,
                        )
                    AuthEntryScreen(
                        serverAddress = serverEndpoint.displayValue,
                        onEditServer = { editServer = true },
                        onSignIn = {
                            viewModel.clearErrors(AuthField.Username, AuthField.Password)
                            navController.navigate(AuthDestination.SignIn.route)
                        },
                        onRegister = {
                            viewModel.clearErrors(
                                AuthField.Username,
                                AuthField.Password,
                                AuthField.ConfirmPassword,
                            )
                            navController.navigate(AuthDestination.Register.route)
                        },
                    )
                }
                composable(AuthDestination.SignIn.route) { backStackEntry ->
                    val viewModel = authViewModel(navController, backStackEntry)
                    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
                    SignInScreen(
                        uiState = uiState,
                        onBack = navController::navigateUp,
                        onSubmit = viewModel::login,
                        onRegister = {
                            viewModel.clearErrors(
                                AuthField.Username,
                                AuthField.Password,
                                AuthField.ConfirmPassword,
                            )
                            navController.navigate(AuthDestination.Register.route)
                        },
                        onFieldChanged = viewModel::clearFieldError,
                    )
                }
                composable(AuthDestination.Register.route) { backStackEntry ->
                    val viewModel = authViewModel(navController, backStackEntry)
                    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
                    RegisterScreen(
                        uiState = uiState,
                        onBack = navController::navigateUp,
                        onSubmit = viewModel::register,
                        onFieldChanged = viewModel::clearFieldError,
                    )
                }
            }
        }
    }
}

@Composable
private fun authViewModel(navController: NavHostController, backStackEntry: NavBackStackEntry): AuthViewModel {
    val graphEntry =
        remember(backStackEntry) {
            navController.getBackStackEntry(AuthDestination.Graph.route)
        }
    return hiltViewModel(graphEntry)
}

@Composable
private fun AuthEffectHandler(
    viewModel: AuthViewModel,
    navController: NavHostController,
    snackbarHostState: SnackbarHostState,
) {
    val context = LocalContext.current
    LaunchedEffect(viewModel, navController, snackbarHostState, context) {
        viewModel.effects.collect { effect ->
            when (effect) {
                is AuthEffect.NavigateToSignIn -> {
                    navController.navigateToSignIn()
                    effect.message?.let { snackbarHostState.showSnackbar(it.resolve(context)) }
                }
                is AuthEffect.ShowMessage -> {
                    snackbarHostState.showSnackbar(effect.message.resolve(context))
                }
            }
        }
    }
}

private fun NavHostController.navigateToSignIn() {
    navigate(AuthDestination.SignIn.route) {
        popUpTo(AuthDestination.Graph.route)
        launchSingleTop = true
    }
}

private fun AuthViewModel.clearErrors(vararg fields: AuthField) {
    fields.forEach(::clearFieldError)
}
