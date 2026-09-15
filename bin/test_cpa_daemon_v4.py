import importlib.util
import math
from pathlib import Path
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "cpa_daemon", Path(__file__).with_name("cpa-daemon-v4.py")
)
daemon = importlib.util.module_from_spec(spec)
spec.loader.exec_module(daemon)


class QuotaDecisionTests(unittest.TestCase):
    def test_quota_above_exhausted_threshold_stays_enabled(self):
        action, _ = daemon.decide(
            {"disabled": False}, {"gemini-weekly": {"frac": 0.03}}, None
        )
        self.assertEqual(action, "keep")

    def test_hysteresis_does_not_reopen_account_below_ten_percent(self):
        action, _ = daemon.decide(
            {"disabled": True}, {"gemini-weekly": {"frac": 0.05}}, None
        )
        self.assertEqual(action, "keep")

    def test_recovered_account_rejoins_at_ten_percent(self):
        action, _ = daemon.decide(
            {"disabled": True}, {"gemini-weekly": {"frac": 0.10}}, None
        )
        self.assertEqual(action, "enable")

    def test_exhausted_weekly_disables_even_with_five_hour_capacity(self):
        action, _ = daemon.decide(
            {"disabled": False},
            {"gemini-weekly": {"frac": 0.02}, "gemini-5h": {"frac": 0.98}},
            None,
        )
        self.assertEqual(action, "disable")

    def test_failed_or_invalid_quota_does_not_change_account_status(self):
        for disabled in (False, True):
            for fraction in (None, -1, 2, "0", False, math.nan, math.inf):
                with self.subTest(disabled=disabled, fraction=fraction):
                    action, _ = daemon.decide(
                        {"disabled": disabled}, {"gemini-weekly": {"frac": fraction}}, None
                    )
                    self.assertEqual(action, "keep")
            for error in ("http429:rate limited", "http503:unavailable", "api-call-no-response"):
                action, _ = daemon.decide({"disabled": disabled}, {}, error)
                self.assertEqual(action, "keep")

    def test_missing_wrapper_status_does_not_mean_dead_credentials(self):
        self.assertFalse(daemon.looks_dead("httpNone:"))
        self.assertTrue(daemon.looks_dead("auth-refresh-failed"))

    def test_quota_target_defaults_to_stable_blue_process(self):
        with patch.dict(daemon.os.environ, {}, clear=True):
            self.assertEqual(daemon._resolve_cpa_url(), "http://cli-proxy-api-blue:8317")

    def test_explicit_quota_target_wins(self):
        with patch.dict(daemon.os.environ, {"CPA_URL": "http://blue:8317"}, clear=True):
            self.assertEqual(daemon._resolve_cpa_url(), "http://blue:8317")

    def test_dry_run_does_not_advance_quarantine_streak(self):
        row = ("account", {"name": "account.json"}, {}, "auth-refresh-failed", "keep", "unreadable")
        with patch.object(daemon, "load_dead_streaks", return_value={}), patch.object(
            daemon, "save_dead_streaks"
        ) as save:
            daemon.apply_quarantine([row], False)
        save.assert_not_called()

    def test_failed_disable_does_not_free_an_enable_slot(self):
        auths = [
            {"email": "a", "name": "a.json", "auth_index": "a", "disabled": False},
            {"email": "b", "name": "b.json", "auth_index": "b", "disabled": True},
        ]
        with patch.object(daemon, "list_antigravity_auths", return_value=auths), patch.object(
            daemon, "fetch_quota", side_effect=[({}, None), ({}, None)]
        ), patch.object(daemon, "decide", side_effect=[("disable", "empty"), ("enable", "healthy")]), patch.object(
            daemon, "apply_quarantine", return_value=0
        ), patch.object(daemon, "MAX_ACTIVE", 1), patch.object(
            daemon, "set_disabled", return_value=False
        ) as set_status:
            daemon.run_cycle(True)
        set_status.assert_called_once_with("a.json", True)


if __name__ == "__main__":
    unittest.main()
